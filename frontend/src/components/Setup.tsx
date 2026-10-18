import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../lib/api";

/**
 * First-run passphrase setup. The code comes from the app's logs, which
 * proves the person here administers the NAS rather than just reached it.
 */
export function Setup({ onDone }: { onDone: () => void }) {
  const [code, setCode] = useState("");
  const [pass, setPass] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const first = useRef<HTMLInputElement>(null);

  useEffect(() => first.current?.focus(), []);

  const mismatch = again !== "" && again !== pass;
  const short = pass !== "" && [...pass].length < 12;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy || mismatch || short || !code || !pass) return;
    setBusy(true);
    setError(null);
    try {
      await api.setup(code, pass);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "That setup code is wrong. Check the app logs." : (err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login">
      <form className="dialog" onSubmit={submit} aria-label="Set up Strata">
        <h3>Set up Strata</h3>
        <p>
          Enter the setup code from the app's logs, then choose the passphrase you'll sign in with. On TrueNAS: Apps →
          Strata → Logs.
        </p>
        <input type="text" name="username" autoComplete="username" value="strata" readOnly hidden />
        <p>Setup code</p>
        <input
          ref={first}
          value={code}
          onChange={(e) => { setCode(e.target.value); setError(null); }}
          autoComplete="off"
          spellCheck={false}
          placeholder="XXXX-XXXX-XXXX-XXXX-XXXX"
          style={{ fontFamily: "var(--mono)" }}
        />
        <p>New passphrase (at least 12 characters)</p>
        <input type="password" name="password" autoComplete="new-password" value={pass} onChange={(e) => { setPass(e.target.value); setError(null); }} />
        <p>Repeat passphrase</p>
        <input type="password" autoComplete="new-password" value={again} onChange={(e) => { setAgain(e.target.value); setError(null); }} />
        {(error || mismatch || short) && (
          <p className="login-error" role="alert">
            {error ?? (short ? "Use at least 12 characters." : "The passphrases don't match.")}
          </p>
        )}
        <div className="dialog-actions">
          <button className="btn primary" type="submit" disabled={busy || !code || !pass || pass !== again || short}>
            {busy ? "Saving…" : "Set passphrase"}
          </button>
        </div>
      </form>
    </div>
  );
}
