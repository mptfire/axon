/**
 * VersionChecker polls the /v1/version endpoint to detect server restarts
 * or configuration changes, prompting users to refresh the page.
 */

const CHECK_INTERVAL = 30 * 1000; // 5 * 60 * 1000; // 5 minutes

class VersionChecker {
  constructor() {
    this.initialConfigHash = null;
    this.listener = null;
    this.intervalId = null;
  }

  /**
   * Starts the version checker worker. It stores the initial config hash
   * from the config.js and polls the server every 5 minutes.
   */
  startWorker() {
    // Store initial config hash from the config loaded at page load
    this.initialConfigHash = window.config?.config_hash || null;

    if (!this.initialConfigHash) {
      console.log("[VersionChecker] No initial config_hash found, version checking disabled");
      return;
    }

    console.log("[VersionChecker] Starting version checker with initial hash:", this.initialConfigHash);

    // Start polling
    this.intervalId = setInterval(() => this.checkVersion(), CHECK_INTERVAL);
  }

  /**
   * Stops the version checker worker.
   */
  stopWorker() {
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
    console.log("[VersionChecker] Stopped version checker");
  }

  /**
   * Registers a listener that will be called when a version change is detected.
   * @param {function} listener - Callback function that receives no arguments
   */
  registerListener(listener) {
    this.listener = listener;
  }

  /**
   * Resets the listener.
   */
  resetListener() {
    this.listener = null;
  }

  /**
   * Fetches the current version from the server and compares it with the initial config hash.
   */
  async checkVersion() {
    if (!this.initialConfigHash) {
      return;
    }

    try {
      const response = await fetch(`${window.config?.base_url || ""}/v1/version`);
      if (!response.ok) {
        console.log("[VersionChecker] Failed to fetch version:", response.status);
        return;
      }

      const data = await response.json();
      const currentHash = data.config_hash;

      console.log("[VersionChecker] Checked version, initial:", this.initialConfigHash, "current:", currentHash);

      if (currentHash && currentHash !== this.initialConfigHash) {
        console.log("[VersionChecker] Config hash changed, notifying listener");
        if (this.listener) {
          this.listener();
        }
      }
    } catch (error) {
      console.log("[VersionChecker] Error checking version:", error);
    }
  }
}

const versionChecker = new VersionChecker();
export default versionChecker;
