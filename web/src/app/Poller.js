import api from "./Api";
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
    const deletedSids = this.deletedSids(notifications);
    const newOrUpdatedNotifications = this.newOrUpdatedNotifications(notifications, deletedSids);

    // Delete all existing notifications with a deleted sequence ID
    if (deletedSids.length > 0) {
      console.log(`[Poller] Deleting notifications with deleted sequence IDs for ${subscription.id}`);
      await Promise.all(deletedSids.map((sid) => subscriptionManager.deleteNotificationBySid(subscription.id, sid)));
    }

    // Add new or updated notifications
    if (newOrUpdatedNotifications.length > 0) {
      console.log(`[Poller] Adding ${notifications.length} notification(s) for ${subscription.id}`);
      await subscriptionManager.addNotifications(subscription.id, notifications);
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

  deletedSids(notifications) {
    return new Set(
      notifications
        .filter(n => n.sid && n.deleted)
        .map(n => n.sid)
    );
  }

  newOrUpdatedNotifications(notifications, deletedSids) {
    return notifications
      .filter((notification) => {
        const sid = notification.sid || notification.id;
        return !deletedSids.has(notification.id) && !deletedSids.has(sid) && !notification.deleted;
      });
  }
}

const poller = new Poller();
export default poller;
