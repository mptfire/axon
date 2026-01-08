import Dexie from "dexie";
import session from "./Session";

// Uses Dexie.js
// https://dexie.org/docs/API-Reference#quick-reference
//
// Notes:
// - As per docs, we only declare the indexable columns, not all columns

const createDatabase = (username) => {
  const dbName = username ? `ntfy-${username}` : "ntfy"; // IndexedDB database is based on the logged-in user
  const db = new Dexie(dbName);

  db.version(3).stores({
    subscriptions: "&id,baseUrl,[baseUrl+mutedUntil]",
    notifications: "&id,sequenceId,subscriptionId,time,new,deleted,[subscriptionId+new],[subscriptionId+sequenceId]",
    users: "&baseUrl,username",
    prefs: "&key"
  });

  return db;
};

export const dbAsync = async () => {
  const username = await session.usernameAsync();
  return createDatabase(username);
};

const db = () => createDatabase(session.username());

export default db;
