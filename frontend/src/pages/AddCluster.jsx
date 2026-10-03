import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { api } from "../api.js";
import { useClusters } from "../clusters.jsx";
import ErrorBox from "../components/ErrorBox.jsx";
import Button from "../components/Button.jsx";
import { Field, Select, Textarea, TextInput } from "../components/Field.jsx";
import { UploadIcon } from "../components/icons.jsx";

// Steps: paste or upload a kubeconfig → we list its contexts → pick one,
// name it → the backend tests the connection and saves it.
export default function AddCluster() {
  const navigate = useNavigate();
  const { reload } = useClusters();

  const [name, setName] = useState("");
  const [kubeconfig, setKubeconfig] = useState("");
  const [contexts, setContexts] = useState([]);
  const [context, setContext] = useState("");
  const [readError, setReadError] = useState(null);
  const [saveError, setSaveError] = useState(null);
  const [saving, setSaving] = useState(false);
  const fileInput = useRef(null);

  // Each time the kubeconfig text changes, ask the backend which contexts it
  // has. We wait 400 ms after the last keystroke so we don't call on every key.
  useEffect(() => {
    if (!kubeconfig.trim()) {
      setContexts([]);
      setReadError(null);
      return;
    }
    const timer = setTimeout(async () => {
      try {
        const result = await api("/clusters/inspect", { method: "POST", body: { kubeconfig } });
        setContexts(result.contexts);
        setContext(result.current || result.contexts[0]?.name || "");
        setReadError(null);
      } catch (err) {
        setContexts([]);
        setReadError(err);
      }
    }, 400);
    return () => clearTimeout(timer);
  }, [kubeconfig]);

  async function loadFile(event) {
    const file = event.target.files[0];
    if (file) setKubeconfig(await file.text());
  }

  async function save(event) {
    event.preventDefault(); // stop the browser from submitting the form itself
    setSaving(true);
    setSaveError(null);
    try {
      const cluster = await api("/clusters", { method: "POST", body: { name, kubeconfig, context } });
      reload();
      navigate(`/c/${cluster.id}/overview`);
    } catch (err) {
      setSaveError(err);
      setSaving(false);
    }
  }

  const chosen = contexts.find((c) => c.name === context);

  return (
    <section className="narrow">
      <h1 className="page-title">Add a cluster</h1>
      <p className="page-about">
        Paste a kubeconfig or load it from a file. Opscope checks that it can connect, then saves only the context
        you pick.
      </p>

      <form onSubmit={save} className="form">
        <Field
          label="Kubeconfig"
          action={
            <>
              <Button variant="quiet" size="sm" icon={UploadIcon} onClick={() => fileInput.current.click()}>
                Load from file
              </Button>
              <input ref={fileInput} type="file" onChange={loadFile} hidden />
            </>
          }
          error={readError?.message}
          hint={
            <>
              Credentials must be embedded in the file. Kubeconfigs that log in with a command (for example cloud CLI
              plugins) can only be used through <code>OPSCOPE_KUBECONFIG</code>. Tip:{" "}
              <code>kubectl config view --minify --flatten</code> prints an embedded copy of your current context.
            </>
          }
        >
          <Textarea
            mono
            rows={12}
            spellCheck={false}
            placeholder={"apiVersion: v1\nkind: Config\nclusters:\n  ..."}
            value={kubeconfig}
            onChange={(e) => setKubeconfig(e.target.value)}
          />
        </Field>

        {contexts.length > 0 && (
          <Field label="Context" hint={chosen && <span className="mono">{chosen.server}</span>}>
            <Select value={context} onChange={(e) => setContext(e.target.value)}>
              {contexts.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name}
                </option>
              ))}
            </Select>
          </Field>
        )}

        <Field label="Display name">
          <TextInput placeholder="e.g. Home lab" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>

        {saveError && <ErrorBox title="Couldn't add the cluster" message={saveError.message} detail={saveError.detail} />}

        <div>
          <Button type="submit" variant="primary" loading={saving} disabled={!context || !name.trim()}>
            Test connection and save
          </Button>
        </div>
      </form>
    </section>
  );
}
