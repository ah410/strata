import { useCallback, useEffect, useState } from "react";
import { api, type TrashItem } from "../lib/api";
import { bytes, parentOf, when } from "../lib/format";
import { Glyph } from "./Glyph";

/** Deleted items, restorable until the retention period purges them. */
export function TrashView({ toast }: { toast: (text: string, error?: boolean) => void }) {
  const [items, setItems] = useState<TrashItem[] | null>(null);
  const [confirmEmpty, setConfirmEmpty] = useState(false);

  const load = useCallback(() => {
    api.trash().then(setItems, (e: Error) => toast(e.message, true));
  }, [toast]);

  useEffect(load, [load]);

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn();
      toast(ok);
    } catch (e) {
      toast((e as Error).message, true);
    }
    load();
  };

  return (
    <>
      <div className="topbar">
        <div className="crumbs">
          <span className="crumb" style={{ cursor: "default" }}>Trash</span>
        </div>
        <div className="actions">
          {items && items.length > 0 && !confirmEmpty && (
            <button className="btn danger" onClick={() => setConfirmEmpty(true)}>
              Empty trash
            </button>
          )}
          {confirmEmpty && (
            <>
              <span className="muted" style={{ alignSelf: "center" }}>Delete everything permanently?</span>
              <button className="btn" onClick={() => setConfirmEmpty(false)}>Cancel</button>
              <button
                className="btn danger"
                onClick={() => {
                  setConfirmEmpty(false);
                  act(() => api.purge("all"), "Trash emptied");
                }}
              >
                Empty
              </button>
            </>
          )}
        </div>
      </div>

      <div className="listing">
        {items && items.length === 0 && (
          <div className="state">
            <h2>Trash is empty</h2>
            <p>Deleted files stay here for 30 days before they're removed for good.</p>
          </div>
        )}
        {items && items.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th className="col-kind">From</th>
                <th className="col-when">Deleted</th>
                <th className="num col-size">Size</th>
                <th style={{ width: 210 }} />
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <tr className="row" key={it.id}>
                  <td>
                    <div className="name">
                      <span className="glyph">
                        <Glyph kind={it.isDir ? "folder" : "file"} />
                      </span>
                      <span>{it.path.split("/").pop()}</span>
                    </div>
                  </td>
                  <td className="col-kind">/{parentOf(it.path)}</td>
                  <td className="col-when">{when(it.deleted)}</td>
                  <td className="col-size">{it.isDir ? "—" : bytes(it.size)}</td>
                  <td style={{ textAlign: "right" }}>
                    <button className="btn" onClick={() => act(() => api.restore(it.id), `Restored ${it.path}`)}>
                      Restore
                    </button>{" "}
                    <button className="btn danger" onClick={() => act(() => api.purge(it.id), "Deleted permanently")}>
                      Delete forever
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
