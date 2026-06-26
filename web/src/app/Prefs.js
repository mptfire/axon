import db from "./db";

export const THEME = {
  DARK: "dark",
  LIGHT: "light",
  SYSTEM: "system",
};

// Default values the getters return when a pref is unset; also used by PrefCache (PrefCache.jsx).
export const PREF_DEFAULTS = {
  sound: "ding",
  minPriority: 1,
  deleteAfter: 604800, // one week
  theme: THEME.SYSTEM,
  webPushEnabled: false,
};

class Prefs {
  constructor(dbImpl) {
    this.db = dbImpl;
  }

  async setSound(sound) {
    this.db.prefs.put({ key: "sound", value: sound.toString() });
  }

  async sound() {
    const sound = await this.db.prefs.get("sound");
    return sound ? sound.value : PREF_DEFAULTS.sound;
  }

  async setMinPriority(minPriority) {
    this.db.prefs.put({ key: "minPriority", value: minPriority.toString() });
  }

  async minPriority() {
    const minPriority = await this.db.prefs.get("minPriority");
    return minPriority ? Number(minPriority.value) : PREF_DEFAULTS.minPriority;
  }

  async setDeleteAfter(deleteAfter) {
    await this.db.prefs.put({ key: "deleteAfter", value: deleteAfter.toString() });
  }

  async deleteAfter() {
    const deleteAfter = await this.db.prefs.get("deleteAfter");
    return deleteAfter ? Number(deleteAfter.value) : PREF_DEFAULTS.deleteAfter;
  }

  async webPushEnabled() {
    const webPushEnabled = await this.db.prefs.get("webPushEnabled");
    return webPushEnabled?.value ?? PREF_DEFAULTS.webPushEnabled;
  }

  async setWebPushEnabled(enabled) {
    await this.db.prefs.put({ key: "webPushEnabled", value: enabled });
  }

  async theme() {
    const theme = await this.db.prefs.get("theme");
    return theme?.value ?? PREF_DEFAULTS.theme;
  }

  async setTheme(mode) {
    await this.db.prefs.put({ key: "theme", value: mode });
  }
}

const prefs = new Prefs(db());
export default prefs;
