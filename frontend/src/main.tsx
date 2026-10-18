import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { Login } from "./components/Login";
import { Setup } from "./components/Setup";
import { api, ApiError, SIGNED_OUT } from "./lib/api";
import "./styles.css";
import "./lib/themes"; // applies the saved theme before first render

/** Shows sign-in until the server accepts a session, and again if it ends. */
function AuthGate() {
  // null while checking; then whether sign-in is enabled and done, and
  // whether the server still needs its first passphrase.
  const [state, setState] = useState<null | { auth: boolean; in: boolean; setup?: boolean }>(null);

  const check = () =>
    api.session().then(
      (auth) => setState({ auth, in: true }),
      (e) => setState({ auth: true, in: false, setup: e instanceof ApiError && e.status === 428 })
    );

  useEffect(() => {
    check();
    const out = () => check();
    window.addEventListener(SIGNED_OUT, out);
    return () => window.removeEventListener(SIGNED_OUT, out);
  }, []);

  if (!state) return null;
  if (state.setup) return <Setup onDone={check} />;
  if (!state.in) return <Login onDone={check} />;
  return (
    <App
      onSignOut={
        state.auth
          ? () => api.logout().finally(() => setState({ auth: true, in: false }))
          : undefined
      }
    />
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AuthGate />
  </StrictMode>
);
