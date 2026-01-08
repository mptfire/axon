import api from "./Api";
import prefs from "./Prefs";
import subscriptionManager from "./SubscriptionManager";

const delayMillis = 2000; // 2 seconds
const intervalMillis = 300000; // 5 minutes

class Poller {
  constructor() {
    this.timer = null;
  }

  startWorker() {
    if (this.timer !== null) {
      return;
    }
    console.log(`[Poller] Starting worker`);
    this.timer = setInterval(() => this.pollAll(), intervalMillis);
    setTimeout(() => this.pollAll(), delayMillis);
  }

  stopWorker() {
    clearTimeout(this.timer);
  }

  async pollAll() {
    console.log(`[Poller] Polling all subscriptions`);
    const subscriptions = await subscriptionManager.all();

    await Promise.all(
      subscriptions.map(async (s) => {
        try {
          await this.poll(s);
        } catch (e) {
          console.log(`[Poller] Error polling ${s.id}`, e);
        }
      })
    );
  }

  async poll(subscription) {
    console.log(`[Poller] Polling ${subscription.id}`);

    const since = subscription.last;
    const notifications = await api.poll(subscription.baseUrl, subscription.topic, since);

    // Filter out notifications older than the prune threshold
    const deleteAfterSeconds = await prefs.deleteAfter();
    const pruneThresholdTimestamp = deleteAfterSeconds > 0 ? Math.round(Date.now() / 1000) - deleteAfterSeconds : 0;
    const recentNotifications = pruneThresholdTimestamp > 0 ? notifications.filter((n) => n.time >= pruneThresholdTimestamp) : notifications;

    // Find the latest notification for each sequence ID
    const latestBySid = this.latestNotificationsBySid(recentNotifications);

    // Delete all existing notifications for which the latest notification is marked as deleted
    const deletedSids = Object.entries(latestBySid)
      .filter(([, notification]) => notification.deleted)
      .map(([sid]) => sid);
    if (deletedSids.length > 0) {
      console.log(`[Poller] Deleting notifications with deleted sequence IDs for ${subscription.id}`, deletedSids);
      await Promise.all(deletedSids.map((sid) => subscriptionManager.deleteNotificationBySid(subscription.id, sid)));
    }

    // Add only the latest notification for each non-deleted sequence
    const notificationsToAdd = Object.values(latestBySid).filter((n) => !n.deleted);
    if (notificationsToAdd.length > 0) {
      console.log(`[Poller] Adding ${notificationsToAdd.length} notification(s) for ${subscription.id}`);
      await subscriptionManager.addNotifications(subscription.id, notificationsToAdd);
    } else {
      console.log(`[Poller] No new notifications found for ${subscription.id}`);
    }
  }

  pollInBackground(subscription) {
    (async () => {
      try {
        await this.poll(subscription);
      } catch (e) {
        console.error(`[App] Error polling subscription ${subscription.id}`, e);
      }
    })();
  }

  /**
   * Groups notifications by sid and returns only the latest (highest time) for each sequence.
   * Returns an object mapping sid -> latest notification.
   */
  latestNotificationsBySid(notifications) {
    const latestBySid = {};
    notifications.forEach((notification) => {
      const sid = notification.sid || notification.id;
      if (!(sid in latestBySid) || notification.time >= latestBySid[sid].time) {
        latestBySid[sid] = notification;
      }
    });
    return latestBySid;
  }
}

const poller = new Poller();
export default poller;
