import accountApi from "./AccountApi";
import subscriptionManager from "./SubscriptionManager";
import poller from "./Poller";
import session from "./Session";
import routes from "../components/routes";
import { UnauthorizedError } from "./errors";

// subscribeTopic subscribes to a topic on the given server and mirrors the subscription
// to the user's account (if logged in). It lives outside the subscribe dialog because
// multiple flows use it: the manual dialog, the AI assistant, and reservations.
export const subscribeTopic = async (baseUrl, topic, opts) => {
  const subscription = await subscriptionManager.upsert(baseUrl, topic, opts);
  if (session.exists()) {
    try {
      await accountApi.addSubscription(baseUrl, topic);
    } catch (e) {
      console.log(`[subscribe] Subscribing to topic ${topic} failed`, e);
      if (e instanceof UnauthorizedError) {
        await session.resetAndRedirect(routes.login);
      }
    }
  }
  return subscription;
};

// pollInBackground is re-exported for convenience so callers of subscribeTopic do not
// need to import the poller separately (single import for the common sequence).
export { poller };
