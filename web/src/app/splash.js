// Fades out and removes the static splash screen baked into index.html (see web/index.html).
// The splash paints before the JS bundle loads to avoid the white flash + spinner flicker on
// first load; the app calls this once it has mounted and the initial data is ready. Idempotent --
// safe to call from multiple routes/effects.

// Keep the splash up for at least this long so it doesn't flash-and-vanish on fast (warm-cache)
// loads -- the logo gets a beat to be seen (and to pulse) before fading out.
const MIN_VISIBLE_MS = 1000;

// Hide in two phases: first fade the (pulsing) logo all the way out, then fade the background away
// to reveal -- "fade in" -- the app underneath. APP_FADE_MS must match the #splash opacity
// transition in index.html.
const LOGO_FADE_MS = 300;
const APP_FADE_MS = 100;

let removed = false;

const fadeOutAndRemove = () => {
  const splash = document.getElementById("splash");
  if (!splash) {
    return;
  }

  // Phase 1: fade the logo out completely. Freeze the pulse at its current opacity first, then
  // transition to 0 -- otherwise stopping the animation would snap the logo to full opacity.
  const img = splash.querySelector("img");
  if (img) {
    const current = getComputedStyle(img).opacity;
    img.style.opacity = current;
    img.style.animation = "none";
    img.getBoundingClientRect(); // force reflow so the fade starts from `current`, not the snapped value
    img.style.transition = `opacity ${LOGO_FADE_MS}ms ease-out`;
    img.style.opacity = "0";
  }

  // Phase 2: once the logo is gone, lift the background to fade the app in, then remove the node.
  setTimeout(() => {
    splash.classList.add("splash-hidden");
    const remove = () => splash.remove();
    // Only react to the background's own opacity transition -- the logo's transitionend bubbles up
    // here too, and would otherwise remove the splash before the app has finished fading in.
    const onEnd = (event) => {
      if (event.target === splash) {
        splash.removeEventListener("transitionend", onEnd);
        remove();
      }
    };
    splash.addEventListener("transitionend", onEnd);
    setTimeout(remove, APP_FADE_MS + 100); // fallback if transitionend never fires
  }, LOGO_FADE_MS);
};

const hideSplash = () => {
  if (removed) {
    return;
  }
  removed = true;
  // performance.now() is the time since the page started loading, i.e. roughly how long the splash
  // has been visible. Hold it until MIN_VISIBLE_MS has elapsed before fading out.
  const remaining = Math.max(0, MIN_VISIBLE_MS - performance.now());
  setTimeout(fadeOutAndRemove, remaining);
};

export default hideSplash;
