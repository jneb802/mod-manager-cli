import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Check,
  FolderOpen,
  Loader2,
  Plus,
  RefreshCcw,
  ShieldAlert,
  Trash2,
  Upload,
} from "lucide-react";
import "./styles.css";

type ProfileSummary = {
  name: string;
  mods: number;
  active: boolean;
};

type State = {
  initialized: boolean;
  configDir: string;
  configFile: string;
  valheimPath: string;
  detectedPath: string;
  activeProfile: string;
  profiles: ProfileSummary[] | null;
};

type Toast = {
  tone: "ok" | "error";
  message: string;
};

const emptyState: State = {
  initialized: false,
  configDir: "",
  configFile: "",
  valheimPath: "",
  detectedPath: "",
  activeProfile: "",
  profiles: [],
};

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const service = window.go?.app?.Service;
  const fn = service?.[method];
  if (!fn) {
    throw new Error("Desktop backend is not connected");
  }
  return (await fn(...args)) as T;
}

function App() {
  const [state, setState] = useState<State>(emptyState);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [toast, setToast] = useState<Toast | null>(null);
  const [valheimPath, setValheimPath] = useState("");
  const [newProfile, setNewProfile] = useState("");
  const [profileCode, setProfileCode] = useState("");

  const profiles = useMemo(() => state.profiles ?? [], [state.profiles]);

  useEffect(() => {
    refresh();
  }, []);

  useEffect(() => {
    if (!valheimPath && state.detectedPath) {
      setValheimPath(state.detectedPath);
    }
  }, [state.detectedPath, valheimPath]);

  async function refresh() {
    setLoading(true);
    try {
      const next = await call<State>("State");
      setState(next);
      setToast(null);
    } catch (error) {
      setToast({ tone: "error", message: errorMessage(error) });
    } finally {
      setLoading(false);
    }
  }

  async function runAction(label: string, action: () => Promise<State | void>, ok: string) {
    setBusy(label);
    setToast(null);
    try {
      const result = await action();
      if (result) {
        setState(result);
      }
      setToast({ tone: "ok", message: ok });
    } catch (error) {
      setToast({ tone: "error", message: errorMessage(error) });
    } finally {
      setBusy("");
    }
  }

  function initialize() {
    runAction(
      "initialize",
      () => call<State>("Initialize", { valheimPath, force: false }),
      "Initialized mmcli"
    );
  }

  function createProfile() {
    const name = newProfile.trim();
    if (!name) {
      setToast({ tone: "error", message: "Profile name is required" });
      return;
    }
    runAction("create", async () => {
      const next = await call<State>("CreateProfile", name);
      setNewProfile("");
      return next;
    }, `Created ${name}`);
  }

  function switchProfile(name: string) {
    runAction("switch:" + name, () => call<State>("SwitchProfile", name), `Activated ${name}`);
  }

  function deleteProfile(name: string) {
    if (!window.confirm(`Delete profile "${name}"?`)) {
      return;
    }
    runAction("delete:" + name, () => call<State>("DeleteProfile", name), `Deleted ${name}`);
  }

  function importProfileCode() {
    const code = profileCode.trim();
    if (!code) {
      setToast({ tone: "error", message: "Profile code is required" });
      return;
    }
    runAction("import", async () => {
      const next = await call<State>("ImportProfileCode", code);
      setProfileCode("");
      return next;
    }, "Imported profile code");
  }

  function openProfile() {
    runAction("open", () => call<void>("OpenActiveProfile"), "Opened profile folder");
  }

  if (loading) {
    return (
      <main className="center">
        <Loader2 className="spin" size={28} />
      </main>
    );
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">m</div>
          <div>
            <h1>mmcli</h1>
            <p>{state.initialized ? state.activeProfile || "profiles" : "setup"}</p>
          </div>
        </div>

        <div className="status-block">
          <span className={state.initialized ? "dot ok" : "dot warn"} />
          <span>{state.initialized ? "Initialized" : "Not initialized"}</span>
        </div>

        <dl className="facts">
          <div>
            <dt>Config</dt>
            <dd>{state.configDir || "-"}</dd>
          </div>
          <div>
            <dt>Valheim</dt>
            <dd>{state.valheimPath || state.detectedPath || "-"}</dd>
          </div>
        </dl>

        <button className="quiet-button" onClick={refresh} disabled={!!busy}>
          <RefreshCcw size={16} />
          Refresh
        </button>
      </aside>

      <section className="workspace">
        {toast && <ToastView toast={toast} />}
        {!state.initialized ? (
          <SetupPanel
            valheimPath={valheimPath}
            setValheimPath={setValheimPath}
            detectedPath={state.detectedPath}
            busy={busy}
            initialize={initialize}
          />
        ) : (
          <ProfilesPanel
            profiles={profiles}
            busy={busy}
            newProfile={newProfile}
            setNewProfile={setNewProfile}
            profileCode={profileCode}
            setProfileCode={setProfileCode}
            createProfile={createProfile}
            importProfileCode={importProfileCode}
            switchProfile={switchProfile}
            deleteProfile={deleteProfile}
            openProfile={openProfile}
          />
        )}
      </section>
    </main>
  );
}

function SetupPanel(props: {
  valheimPath: string;
  setValheimPath: (value: string) => void;
  detectedPath: string;
  busy: string;
  initialize: () => void;
}) {
  return (
    <div className="panel">
      <div className="panel-head">
        <div>
          <h2>First Run</h2>
          <p>BepInEx and the default profile will be prepared for this Valheim install.</p>
        </div>
      </div>

      <div className="field">
        <label htmlFor="valheimPath">Valheim install path</label>
        <input
          id="valheimPath"
          value={props.valheimPath}
          onChange={(event) => props.setValheimPath(event.target.value)}
          placeholder="/Users/name/Library/Application Support/Steam/steamapps/common/Valheim"
        />
      </div>

      {props.detectedPath && (
        <button className="inline-button" onClick={() => props.setValheimPath(props.detectedPath)}>
          <Check size={16} />
          Use detected path
        </button>
      )}

      <div className="action-row">
        <button
          className="primary-button"
          disabled={props.busy === "initialize"}
          onClick={props.initialize}
        >
          {props.busy === "initialize" ? <Loader2 className="spin" size={17} /> : <Check size={17} />}
          Initialize
        </button>
      </div>
    </div>
  );
}

function ProfilesPanel(props: {
  profiles: ProfileSummary[];
  busy: string;
  newProfile: string;
  setNewProfile: (value: string) => void;
  profileCode: string;
  setProfileCode: (value: string) => void;
  createProfile: () => void;
  importProfileCode: () => void;
  switchProfile: (name: string) => void;
  deleteProfile: (name: string) => void;
  openProfile: () => void;
}) {
  return (
    <div className="profile-layout">
      <section className="panel profile-list-panel">
        <div className="panel-head">
          <div>
            <h2>Profiles</h2>
            <p>{props.profiles.length} profile{props.profiles.length === 1 ? "" : "s"}</p>
          </div>
          <button className="icon-button" onClick={props.openProfile} disabled={!!props.busy} title="Open folder">
            <FolderOpen size={17} />
          </button>
        </div>

        <div className="profile-list">
          {props.profiles.map((profile) => (
            <div className={profile.active ? "profile-row active" : "profile-row"} key={profile.name}>
              <div className="profile-main">
                <span className="profile-name">{profile.name}</span>
                <span className="profile-meta">{profile.mods} mods</span>
              </div>
              <div className="profile-actions">
                {profile.active ? (
                  <span className="active-pill">Active</span>
                ) : (
                  <button
                    className="small-button"
                    onClick={() => props.switchProfile(profile.name)}
                    disabled={!!props.busy}
                  >
                    {props.busy === "switch:" + profile.name ? (
                      <Loader2 className="spin" size={15} />
                    ) : (
                      <Check size={15} />
                    )}
                    Activate
                  </button>
                )}
                <button
                  className="danger-icon"
                  onClick={() => props.deleteProfile(profile.name)}
                  disabled={profile.active || !!props.busy}
                  title="Delete profile"
                >
                  {props.busy === "delete:" + profile.name ? (
                    <Loader2 className="spin" size={15} />
                  ) : (
                    <Trash2 size={15} />
                  )}
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="panel tools-panel">
        <div className="tool-group">
          <h3>Create Profile</h3>
          <div className="input-row">
            <input
              value={props.newProfile}
              onChange={(event) => props.setNewProfile(event.target.value)}
              placeholder="profile name"
              onKeyDown={(event) => {
                if (event.key === "Enter") props.createProfile();
              }}
            />
            <button className="primary-button compact" onClick={props.createProfile} disabled={!!props.busy}>
              {props.busy === "create" ? <Loader2 className="spin" size={16} /> : <Plus size={16} />}
              Create
            </button>
          </div>
        </div>

        <div className="tool-group">
          <h3>Import Profile Code</h3>
          <div className="input-row">
            <input
              value={props.profileCode}
              onChange={(event) => props.setProfileCode(event.target.value)}
              placeholder="00000000-0000-0000-0000-000000000000"
              onKeyDown={(event) => {
                if (event.key === "Enter") props.importProfileCode();
              }}
            />
            <button className="secondary-button compact" onClick={props.importProfileCode} disabled={!!props.busy}>
              {props.busy === "import" ? <Loader2 className="spin" size={16} /> : <Upload size={16} />}
              Import
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}

function ToastView({ toast }: { toast: Toast }) {
  return (
    <div className={toast.tone === "ok" ? "toast ok" : "toast error"}>
      {toast.tone === "ok" ? <Check size={16} /> : <ShieldAlert size={16} />}
      <span>{toast.message}</span>
    </div>
  );
}

function errorMessage(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
