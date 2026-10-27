import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../lib/api";

/** Passphrase sign-in. The hidden username lets password managers save it. */
export function Login({ onDone }: { onDone: () => void }) {
  const [pass, setPass] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => input.current?.focus(), []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pass || busy) return;
    setBusy(true);
    setError(null);
    try {
      await api.login(pass);
      setPass("");
      onDone();
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "Wrong passphrase." : (err as Error).message);
      input.current?.select();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login">
      <form className="dialog" onSubmit={submit} aria-label="Sign in">
        <div className="rail-head" style={{ padding: "0 0 12px" }}>
          <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
            <rect x="1.5" y="2.5" width="13" height="3.4" rx="1" fill="var(--k-folder)" opacity=".85" />
            <rect x="1.5" y="6.3" width="13" height="3.4" rx="1" fill="var(--k-sheet)" opacity=".7" />
            <rect x="1.5" y="10.1" width="13" height="3.4" rx="1" fill="var(--k-video)" opacity=".55" />
          </svg>
          <span className="wordmark">Strata</span>
        </div>
        <input type="text" name="username" autoComplete="username" value="strata" readOnly hidden />
        <p>Passphrase</p>
        <input
          ref={input}
          type="password"
          name="password"
          autoComplete="current-password"
          value={pass}
          onChange={(e) => setPass(e.target.value)}
          aria-invalid={error ? true : undefined}
        />
        {error && <p className="login-error" role="alert">{error}</p>}
        <p className="hint">
          Forgot it? Delete <code>auth.hash</code> from the config storage and restart the app for a new setup code.
        </p>
        <div className="dialog-actions">
          <button className="btn primary" type="submit" disabled={busy || !pass}>
            {busy ? "Checking…" : "Sign in"}
          </button>
        </div>
      </form>
    </div>
  );
}
