// Fade transitions for navigating between the main app and the auth pages (login/signup/reset).
// We fade the whole app (#root) out, then either navigate client-side and fade back in, or do a
// full reload (where the splash screen in index.html fades the next page in).

const FADE_MS = 150;

// Fade #root out and return it (or null if not found / no document).
const fadeOutRoot = () => {
  const node = document.getElementById("root");
  if (node) {
    node.style.transition = `opacity ${FADE_MS}ms ease-out`;
    node.style.opacity = "0";
  }
  return node;
};

// Fade the app out, run a client-side navigation, then fade the new page back in. Used for
// app -> login/signup, which stay within the same document (no reload).
export const fadeNavigate = (navigate, to) => {
  const node = fadeOutRoot();
  if (!node) {
    navigate(to);
    return;
  }
  setTimeout(() => {
    navigate(to);
    // Fade back in on the next frame, once the new page has rendered.
    requestAnimationFrame(() => {
      node.style.opacity = "1";
    });
  }, FADE_MS);
};

// Fade the app out, then do a full page reload to `url`. Used for login/signup -> app, which must
// reload (the per-user IndexedDB changes). The splash screen fades the reloaded app back in.
export const fadeReload = (url) => {
  if (!fadeOutRoot()) {
    window.location.href = url;
    return;
  }
  setTimeout(() => {
    window.location.href = url;
  }, FADE_MS);
};
