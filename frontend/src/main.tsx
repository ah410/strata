import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { Login } from "./components/Login";
import { api, SIGNED_OUT } from "./lib/api";
import "./styles.css";
import "./lib/themes"; // applies the saved theme before first render

/** Shows sign-in until the server accepts a session, and again if it ends. */
function AuthGate() {
  // null while checking; then whether sign-in is enabled and done.
  const [state, setState] = useState<null | { auth: boolean; in: boolean }>(null);

  const check = () =>
    api.session().then(
      (auth) => setState({ auth, in: true }),
      () => setState({ auth: true, in: false })
    );

  useEffect(() => {
    check();
    const out = () => setState({ auth: true, in: false });
    window.addEventListener(SIGNED_OUT, out);
    return () => window.removeEventListener(SIGNED_OUT, out);
  }, []);

  if (!state) return null;
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
