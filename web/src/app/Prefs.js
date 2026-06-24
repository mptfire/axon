import db from "./db";

export const THEME = {
  DARK: "dark",
  LIGHT: "light",
  SYSTEM: "system",
};

// Key under which the theme preference is mirrored to localStorage. The inline script in
// index.html reads it synchronously to pick the splash background before first paint.
export const THEME_LOCALSTORAGE_KEY = "theme";

const mirrorThemeToLocalStorage = (value) => {
  try {
    localStorage.setItem(THEME_LOCALSTORAGE_KEY, value);
  } catch (e) {
    // localStorage may be unavailable (private mode, disabled cookies); the splash just falls
    // back to the system color scheme in that case.
  }
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
    return sound ? sound.value : "ding";
  }

  async setMinPriority(minPriority) {
    this.db.prefs.put({ key: "minPriority", value: minPriority.toString() });
  }

  async minPriority() {
    const minPriority = await this.db.prefs.get("minPriority");
    return minPriority ? Number(minPriority.value) : 1;
  }

  async setDeleteAfter(deleteAfter) {
    await this.db.prefs.put({ key: "deleteAfter", value: deleteAfter.toString() });
  }

  async deleteAfter() {
    const deleteAfter = await this.db.prefs.get("deleteAfter");
    return deleteAfter ? Number(deleteAfter.value) : 604800; // Default is one week
  }

  async webPushEnabled() {
    const webPushEnabled = await this.db.prefs.get("webPushEnabled");
    return webPushEnabled?.value;
  }

  async setWebPushEnabled(enabled) {
    await this.db.prefs.put({ key: "webPushEnabled", value: enabled });
  }

  async theme() {
    const theme = await this.db.prefs.get("theme");
    const value = theme?.value ?? THEME.SYSTEM;
    // Mirror to localStorage so the inline script in index.html can pick the splash background
    // synchronously before first paint. Self-heals for users who set their theme before the
    // mirror existed.
    mirrorThemeToLocalStorage(value);
    return value;
  }

  async setTheme(mode) {
    await this.db.prefs.put({ key: "theme", value: mode });
    mirrorThemeToLocalStorage(mode);
  }
}

const prefs = new Prefs(db());
export default prefs;
