package server

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"heckel.io/ntfy/v2/ai"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/metrics"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/user"
	"heckel.io/ntfy/v2/util"
)

// Scheduled daily briefings (axon, Phase 4). Users opt in via their account settings
// (prefs.digest); once a day, at their chosen UTC hour, the server gathers recent
// messages across their subscriptions on this server, summarizes them, and delivers the
// briefing to a private per-user topic that is provisioned (subscription + read ACL)
// alongside the first delivery.

const (
	digestTopicPrefix     = "dg_"                // axon: digest topics are dg_<random> (unguessable, like sync topics)
	digestTopicRandomLen  = 16                   // axon: random part of the digest topic
	digestMessagePriority = 3                    // axon: default priority for briefing messages
	digestHourFallback    = 8                    // axon: default UTC hour if unset
	digestSinceFallback   = 24                   // axon: default window in hours
	digestDayFormat       = "2006-01-02"         // axon: last-daily marker format (UTC)
	digestUserBudgetKey   = "user:%s"            // axon: budget attribution for scheduler-initiated calls
	digestMaxTopics       = ai.BriefingMaxTopics // Same cost cap as on-demand briefings
)

// runDigestScheduler ticks once a minute and delivers due briefings. It runs only when
// the AI layer is enabled and a user manager exists; otherwise it exits immediately.
func (s *Server) runDigestScheduler() {
	if !s.ai.Enabled() || s.userManager == nil {
		return
	}
	log.Tag(tagAI).Debug("Starting daily briefing scheduler")
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.closeChan:
			return
		case <-ticker.C:
			s.sendDueBriefings()
		}
	}
}

// sendDueBriefings delivers briefings for all users whose scheduled hour is now (UTC)
// and who have not been served today.
func (s *Server) sendDueBriefings() {
	users, err := s.userManager.Users()
	if err != nil {
		log.Tag(tagAI).Err(err).Warn("Cannot list users for daily briefings")
		return
	}
	now := time.Now().UTC()
	today := now.Format(digestDayFormat)
	for _, u := range users {
		if !digestDue(u, now, today) {
			continue
		}
		if err := s.sendDailyBriefing(u, today); err != nil {
			log.Tag(tagAI).Err(err).Field("user", u.Name).Warn("Daily briefing failed")
			continue
		}
		log.Tag(tagAI).Field("user", u.Name).Info("Daily briefing delivered")
	}
}

// digestDue reports whether the user's briefing is due this hour and hasn't been
// delivered today.
func digestDue(u *user.User, now time.Time, today string) bool {
	if u == nil || u.Prefs == nil || u.Prefs.Digest == nil || u.Prefs.Digest.Enabled == nil || !*u.Prefs.Digest.Enabled {
		return false
	}
	hour := digestHourFallback
	if u.Prefs.Digest.Hour != nil {
		hour = *u.Prefs.Digest.Hour
	}
	if now.Hour() != hour {
		return false
	}
	return u.Prefs.Digest.LastDaily == nil || *u.Prefs.Digest.LastDaily != today
}

// sendDailyBriefing gathers, generates, delivers, and marks today's briefing for one
// user. Quiet days are marked but not delivered.
func (s *Server) sendDailyBriefing(u *user.User, today string) error {
	sinceHours := digestSinceFallback
	if u.Prefs.Digest.SinceHours != nil {
		sinceHours = *u.Prefs.Digest.SinceHours
	}
	sinceMarker := model.NewSinceTime(time.Now().Add(-1 * time.Duration(sinceHours) * time.Hour).Unix())
	topics, totalMessages, err := s.gatherBriefingTopics(u, sinceMarker)
	if err != nil {
		return err
	}
	if totalMessages == 0 {
		return s.markBriefingSent(u, today) // Quiet day: mark, don't ping
	}
	budgetKey := fmt.Sprintf(digestUserBudgetKey, u.ID)
	briefing, err := ai.NewBriefinger(s.ai).Briefing(briefingContext(), budgetKey, &ai.BriefingInput{Topics: topics})
	if err != nil {
		return err
	}
	if err := s.ensureDigestDelivery(u); err != nil {
		return err
	}
	if err := s.deliverBriefing(u, briefing); err != nil {
		return err
	}
	return s.markBriefingSent(u, today)
}

// briefingContext returns a detached context for scheduler-initiated AI calls (there is
// no request to inherit from). The AI client applies its own per-request timeout.
func briefingContext() context.Context {
	return context.Background()
}

// gatherBriefingTopics collects recent messages across the user's subscriptions on this
// server. Extracted from handleAIBriefing so the scheduler reuses the same logic.
func (s *Server) gatherBriefingTopics(u *user.User, sinceMarker model.SinceMarker) ([]ai.BriefingTopicMessages, int, error) {
	topics := make([]ai.BriefingTopicMessages, 0)
	totalMessages := 0
	if u.Prefs == nil {
		return topics, totalMessages, nil
	}
	for _, sub := range u.Prefs.Subscriptions {
		if len(topics) >= digestMaxTopics {
			break
		}
		if sub.Topic == "" || (sub.BaseURL != "" && sub.BaseURL != s.config.BaseURL) {
			continue
		}
		cachedMessages, _, err := s.messageCache.MessagesCapped(sub.Topic, sinceMarker, false, 128*1024)
		if err != nil {
			return nil, 0, err
		}
		if len(cachedMessages) == 0 {
			continue
		}
		topics = append(topics, ai.BriefingTopicMessages{Topic: sub.Topic, Messages: toDigestMessages(cachedMessages)})
		totalMessages += len(cachedMessages)
	}
	return topics, totalMessages, nil
}

// ensureDigestDelivery provisions the private digest topic: an unguessable topic stored
// in the user's prefs, a read-only ACL grant, and a synced subscription entry so the
// web app (and any client with the user's credentials) receives it.
func (s *Server) ensureDigestDelivery(u *user.User) error {
	if u.Prefs == nil {
		u.Prefs = &user.Prefs{}
	}
	if u.Prefs.Digest == nil {
		u.Prefs.Digest = &user.DigestPrefs{}
	}
	if u.Prefs.Digest.Topic == nil || !topicRegex.MatchString(*u.Prefs.Digest.Topic) {
		topic := util.RandomStringPrefix(digestTopicPrefix, digestTopicRandomLen+len(digestTopicPrefix))
		u.Prefs.Digest.Topic = &topic
	}
	if err := s.userManager.AllowAccess(u.Name, *u.Prefs.Digest.Topic, user.PermissionRead); err != nil {
		return err
	}
	known := false
	for _, sub := range u.Prefs.Subscriptions {
		if sub.BaseURL == s.config.BaseURL && sub.Topic == *u.Prefs.Digest.Topic {
			known = true
			break
		}
	}
	if !known {
		u.Prefs.Subscriptions = append(u.Prefs.Subscriptions, &user.Subscription{BaseURL: s.config.BaseURL, Topic: *u.Prefs.Digest.Topic})
	}
	return s.userManager.ChangeSettings(u.ID, u.Prefs)
}

// deliverBriefing renders the briefing and dispatches it like a delayed message would
// be: fan-out to all connected subscribers (web, Firebase, web push) plus the cache.
func (s *Server) deliverBriefing(u *user.User, briefing *ai.BriefingResult) error {
	if u.Prefs == nil || u.Prefs.Digest == nil || u.Prefs.Digest.Topic == nil {
		return fmt.Errorf("digest topic not provisioned")
	}
	topic := *u.Prefs.Digest.Topic
	v := s.visitor(netip.IPv4Unspecified(), u)
	s.mu.RLock()
	t := s.topics[topic] // May be nil if there are no live subscribers; dispatch handles that
	s.mu.RUnlock()
	m := &model.Message{
		ID:       model.GenerateMessageID(),
		Time:     time.Now().Unix(),
		Event:    model.MessageEvent,
		Topic:    topic,
		Title:    "Daily briefing",
		Message:  renderBriefing(briefing),
		Priority: digestMessagePriority,
		Tags:     []string{"newspaper"},
	}
	if err := s.dispatch(v, t, m, dispatchOpts{firebase: true, webPush: true, upstream: true, async: true}); err != nil {
		return err
	}
	if err := s.messageCache.AddMessage(m); err != nil {
		return err
	}
	metrics.AIBriefingsDelivered.WithLabelValues("daily").Inc()
	return nil
}

// renderBriefing renders the structured briefing as plain text for the message body.
func renderBriefing(briefing *ai.BriefingResult) string {
	var sb strings.Builder
	sb.WriteString(briefing.Headline)
	for _, section := range briefing.Sections {
		sb.WriteString("\n\n")
		if section.Topic != "" && section.Topic != "_" {
			fmt.Fprintf(&sb, "[%s] ", section.Topic)
		}
		sb.WriteString(section.Title)
		for _, point := range section.Points {
			sb.WriteString("\n- ")
			sb.WriteString(point)
		}
	}
	return sb.String()
}

// markBriefingSent persists today's date so the scheduler does not fire twice a day.
func (s *Server) markBriefingSent(u *user.User, today string) error {
	if u.Prefs == nil {
		u.Prefs = &user.Prefs{}
	}
	if u.Prefs.Digest == nil {
		u.Prefs.Digest = &user.DigestPrefs{}
	}
	u.Prefs.Digest.LastDaily = &today
	return s.userManager.ChangeSettings(u.ID, u.Prefs)
}
