import { cloneElement, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { api } from "../api.js";
import { useClusters } from "../clusters.jsx";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import { Field, Select, Textarea, TextInput } from "../components/Field.jsx";
import { Spinner } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { CheckIcon, UploadIcon } from "../components/icons.jsx";
import "./Clusters.css";

// Three steps: give a kubeconfig (paste, upload or drop) → we list its
// contexts → pick one and name it. Saving makes the backend test the
// connection first, and keep only that context.
export default function AddCluster() {
  const navigate = useNavigate();
  const { reload } = useClusters();

  const [kubeconfig, setKubeconfig] = useState("");
  const [contexts, setContexts] = useState([]);
  const [context, setContext] = useState("");
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false); // stop suggesting once they've typed one
  const [readError, setReadError] = useState(null);
  const [saveError, setSaveError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [dragging, setDragging] = useState(false);
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

  // Suggest a display name from the context, until they type their own.
  useEffect(() => {
    if (!nameTouched) setName(suggestName(context));
  }, [context, nameTouched]);

  async function readFile(file) {
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
  const steps = { kubeconfig: contexts.length > 0, context: Boolean(chosen), name: Boolean(name.trim()) };

  return (
    <section className="cluster-page add-cluster">
      <PageHeader
        title="Add a cluster"
        description="Paste a kubeconfig, or drop the file here. Opscope checks that it can connect, then saves only the context you pick."
      />

      <form onSubmit={save} className="add-form">
        <Step number={1} done={steps.kubeconfig}>
          <Field
            label="Kubeconfig"
            action={
              <>
                <Button variant="quiet" size="sm" icon={UploadIcon} onClick={() => fileInput.current.click()}>
                  Load from file
                </Button>
                <input ref={fileInput} type="file" onChange={(e) => readFile(e.target.files[0])} hidden />
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
            <DropZone dragging={dragging} setDragging={setDragging} onFile={readFile}>
              <Textarea
                mono
                rows={10}
                spellCheck={false}
                placeholder={"Paste a kubeconfig here, or drop the file.\n\napiVersion: v1\nkind: Config\n..."}
                value={kubeconfig}
                onChange={(e) => setKubeconfig(e.target.value)}
              />
            </DropZone>
          </Field>
        </Step>

        <Step number={2} done={steps.context}>
          {contexts.length > 0 ? (
            <Field label="Context" hint={chosen && <span className="mono">{chosen.server}</span>}>
              <Select value={context} onChange={(e) => setContext(e.target.value)}>
                {contexts.map((c) => (
                  <option key={c.name} value={c.name}>
                    {c.name}
                  </option>
                ))}
              </Select>
            </Field>
          ) : (
            <div className="add-step-waiting">
              <span className="field-label">Context</span>
              <span>The kubeconfig's contexts appear here once it's read.</span>
            </div>
          )}
        </Step>

        <Step number={3} done={steps.name} last>
          <Field label="Display name" hint="Shown in the cluster switcher, and used in the address of its pages.">
            <TextInput
              placeholder="e.g. Home lab"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setNameTouched(true);
              }}
            />
          </Field>
        </Step>

        {saveError && (
          <Callout tone="error" title="Couldn't add the cluster" detail={saveError.detail}>
            {saveError.message}
          </Callout>
        )}

        <div className="add-actions">
          <Button type="submit" variant="primary" loading={saving} disabled={!steps.context || !steps.name}>
            Test connection and save
          </Button>
          <Button to="/clusters" variant="quiet" disabled={saving}>
            Cancel
          </Button>
          {saving && (
            <span className="add-progress" role="status">
              <Spinner size={14} /> Connecting to <span className="mono">{chosen?.server}</span>… this can take up to 10
              seconds.
            </span>
          )}
        </div>
      </form>
    </section>
  );
}

// One numbered step, with a tick once it's done.
function Step({ number, done, last = false, children }) {
  return (
    <div className={`add-step ${done ? "is-done" : ""} ${last ? "is-last" : ""}`}>
      <span className="add-step-marker" aria-hidden="true">
        {done ? <CheckIcon size={14} /> : number}
      </span>
      <div className="add-step-body">{children}</div>
    </div>
  );
}

// Lets a kubeconfig file be dropped onto the text box. The props Field
// gives its control (id, aria-describedby, invalid) are passed on to the box.
function DropZone({ dragging, setDragging, onFile, children, ...controlProps }) {
  return (
    <div
      className={`drop-zone ${dragging ? "is-dragging" : ""}`}
      onDragOver={(e) => {
        e.preventDefault(); // allows dropping
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        onFile(e.dataTransfer.files[0]);
      }}
    >
      {cloneElement(children, controlProps)}
      {dragging && <div className="drop-zone-hint">Drop the kubeconfig to read it</div>}
    </div>
  );
}

// A friendlier name than the raw context: "kubernetes-admin@kubernetes"
// becomes "kubernetes"; "kind-lab" stays as it is.
function suggestName(context) {
  return context.includes("@") ? context.split("@").pop() : context;
}
