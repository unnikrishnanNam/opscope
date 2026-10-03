import { useState } from "react";
import { api } from "../api.js";
import Button from "./Button.jsx";
import { Tag } from "./Tag.jsx";
import { CopyIcon, EyeIcon, EyeOffIcon } from "./icons.jsx";

// The keys of one secret. Values stay hidden until "Reveal" is clicked for
// that one key; only then is the value fetched from the backend. Nothing is
// saved: hiding the value, collapsing the row or leaving the page forgets it.
export default function SecretKeys({ cluster, secret }) {
  if (secret.keys.length === 0) return <p className="muted">This secret has no keys.</p>;

  return (
    <div className="secret-keys">
      <p className="field-hint">
        Values are fetched one at a time when you reveal them, and aren't stored by Opscope.
      </p>
      {secret.keys.map((key) => (
        <SecretKey key={key} cluster={cluster} secret={secret} name={key} />
      ))}
    </div>
  );
}

function SecretKey({ cluster, secret, name }) {
  const [value, setValue] = useState(null); // { value, base64 } once revealed
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  async function reveal() {
    setLoading(true);
    setError(null);
    try {
      // Every part goes through encodeURIComponent so odd names can't break the URL.
      const path = [cluster.id, "secrets", secret.namespace, secret.name, name].map(encodeURIComponent).join("/");
      setValue(await api(`/clusters/${path}`));
    } catch (err) {
      setError(err);
    }
    setLoading(false);
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(value.value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setError(new Error("The browser didn't allow copying. Select the text instead."));
    }
  }

  return (
    <div className="secret-key">
      <div className="secret-key-row">
        <span className="mono secret-key-name">{name}</span>
        {value ? (
          <>
            {value.base64 && <Tag>binary, shown as base64</Tag>}
            <Button variant="quiet" size="sm" icon={CopyIcon} onClick={copy}>
              {copied ? "Copied" : "Copy"}
            </Button>
            <Button variant="quiet" size="sm" icon={EyeOffIcon} onClick={() => setValue(null)}>
              Hide
            </Button>
          </>
        ) : (
          <>
            <span className="secret-mask" aria-label="hidden value">
              ••••••••
            </span>
            <Button variant="quiet" size="sm" icon={EyeIcon} onClick={reveal} loading={loading}>
              Reveal
            </Button>
          </>
        )}
      </div>
      {value && <pre className="secret-value">{value.value}</pre>}
      {error && <div className="field-error">{error.message}</div>}
    </div>
  );
}
