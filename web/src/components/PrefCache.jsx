import * as React from "react";
import { createContext, useContext } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import prefs, { PREF_DEFAULTS } from "../app/Prefs";

// A render-time CACHE of the user's preferences -- NOT the source of truth. The source of truth is
// the `prefs` table in IndexedDB, always read/written via Prefs.js. This context preloads all prefs
// once (mounted by Layout, above the routed content), so the Settings page renders its controls
// with the right values immediately instead of each one flickering in as its own IndexedDB read
// resolves. By the time you navigate to Settings the query has already resolved.
const PrefCacheContext = createContext(undefined);

export const PrefCacheProvider = ({ children }) => {
  const cache = useLiveQuery(async () => ({
    sound: await prefs.sound(),
    minPriority: await prefs.minPriority(),
    deleteAfter: await prefs.deleteAfter(),
    theme: await prefs.theme(),
    webPushEnabled: await prefs.webPushEnabled(),
  }));
  return <PrefCacheContext.Provider value={cache}>{children}</PrefCacheContext.Provider>;
};

// While the cache is still loading (only on the very first paint, mostly hidden by the splash) fall
// back to the same defaults the Prefs getters use, so controls never render with no value.
export const usePrefCache = () => useContext(PrefCacheContext) ?? PREF_DEFAULTS;
