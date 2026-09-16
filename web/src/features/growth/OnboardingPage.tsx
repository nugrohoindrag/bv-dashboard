// Onboarding (Website PRD §25–§30): Create Organization → Select Property Profile → Create Property → Trial Workspace →
// progressive checklist (+ optional sample data). Property menentukan konteks operasional (profile), bukan organization.
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, Field, Icon, Input, NativeSelect } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { PROFILE_ICON, PROFILE_LABEL, type ProfileCode } from "@/lib/profile";
import { track, useOnboarding, type Onboarding } from "@/lib/growth";
import { cn } from "@/lib/utils";
import { fmtDate } from "@/lib/format";

const PROFILE_DESC: Record<ProfileCode, { blurb: string; bullets: string[] }> = {
  hotel: { blurb: "Rooms, guests, housekeeping turnover, and front desk.", bullets: ["Room status and turnover cleaning", "Guest requests and reception", "Hotel booking management"] },
  apartment: { blurb: "Residents, units, and everyday building services.", bullets: ["Resident requests via the Tenant App", "Unit sales and rental management", "Facility booking and visitors"] },
  office: { blurb: "Tenants, floors, and shared building facilities.", bullets: ["Tenant requests and SLA tracking", "Access and visitor management", "Billing and tenant relation"] },
};

const TIMEZONES = ["Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura", "Asia/Singapore", "Asia/Kuala_Lumpur", "Asia/Bangkok", "Asia/Manila", "Asia/Dubai", "Europe/London", "America/New_York"];

export default function OnboardingPage() {
  const ob = useOnboarding();
  return (
    <div className="mx-auto max-w-4xl">
      <AsyncState query={ob}>{(data) => (data.property_id ? <ChecklistView data={data} onChanged={() => ob.refetch()} /> : <Wizard onDone={() => ob.refetch()} />)}</AsyncState>
    </div>
  );
}

// ---------- Wizard ----------
function Wizard({ onDone }: { onDone: () => void }) {
  const { can, refreshPrincipal, setPropertyId } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const [step, setStep] = useState(0);
  const [orgName, setOrgName] = useState("");
  const [orgLoaded, setOrgLoaded] = useState(false);
  const [profile, setProfile] = useState<ProfileCode | null>(null);
  const [prop, setProp] = useState({ name: "", address: "", city: "", timezone: "Asia/Jakarta", contact_phone: "", contact_email: "" });
  const [busy, setBusy] = useState(false);
  const canCreate = can("property.properties.create");

  useEffect(() => {
    api<{ name: string }>("organizations/me").then((o) => { setOrgName(o.name); setOrgLoaded(true); }).catch(() => setOrgLoaded(true));
  }, []);

  if (!canCreate) {
    return (
      <>
        <PageHeader title="Welcome to BuildingVision" subtitle="Your workspace is ready, but a property has not been created yet." />
        <Alert variant="info" title="Ask your organization admin">Creating the first property needs the Organization Admin role. Once a property exists, you will see your checklist here.</Alert>
      </>
    );
  }

  const saveOrg = async () => {
    if (!orgName.trim()) return toast.error(new Error("Please enter your organization name"));
    setBusy(true);
    try {
      await api("organizations/me", { method: "PATCH", body: { name: orgName.trim() } });
      setStep(1);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };

  const chooseProfile = (p: ProfileCode) => {
    setProfile(p);
    track("profile_selected", { profile: p });
  };

  const createProperty = async () => {
    if (!profile) return;
    if (!prop.name.trim()) return toast.error(new Error("Please give your property a name"));
    setBusy(true);
    try {
      const details: Record<string, unknown> = { profile, timezone: prop.timezone, address: prop.address || undefined, city: prop.city || undefined, property_type: profile };
      const created = await api<{ id: string }>("properties", { body: { name: prop.name.trim(), details } });
      if (prop.contact_phone || prop.contact_email) {
        // settings organization diganti utuh oleh PATCH: gabungkan dengan nilai terkini agar flag onboarding tidak hilang
        await api<{ settings: Record<string, unknown> }>("organizations/me")
          .then((o) => api("organizations/me", { method: "PATCH", body: { settings: { ...(o.settings ?? {}), contact: { phone: prop.contact_phone || undefined, email: prop.contact_email || undefined } } } }))
          .catch(() => undefined);
      }
      track("property_created", { profile });
      await refreshPrincipal();
      setPropertyId(created.id);
      qc.invalidateQueries({ queryKey: ["onboarding"] });
      toast.success(`${prop.name} created with the ${PROFILE_LABEL[profile]} profile`);
      onDone();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };

  const steps = ["Organization", "Property profile", "Property details"];
  return (
    <>
      <PageHeader title="Let's set up your workspace" subtitle="Three short steps. You can change everything later in Settings." />
      <ol className="mb-6 flex items-center gap-2 text-sm">
        {steps.map((s, i) => (
          <li key={s} className="flex items-center gap-2">
            <span className={cn("flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold", i < step ? "bg-primary text-on-primary" : i === step ? "bg-primary-soft text-primary ring-2 ring-primary" : "bg-surface-container text-on-surface-variant")}>{i < step ? <Icon name="check" size={14} /> : i + 1}</span>
            <span className={cn(i === step ? "font-semibold text-on-surface" : "text-on-surface-variant")}>{s}</span>
            {i < steps.length - 1 && <span className="mx-1 h-px w-8 bg-border" />}
          </li>
        ))}
      </ol>

      {step === 0 && (
        <Card className="p-6">
          <CardHeader><CardTitle>Your organization</CardTitle><CardSubtitle>This is the company or management group that runs your properties.</CardSubtitle></CardHeader>
          <CardContent className="mt-4 space-y-4">
            <Field label="Organization name" required>
              <Input value={orgName} onChange={(e) => setOrgName(e.target.value)} disabled={!orgLoaded} placeholder="Pangeran Property Group" />
            </Field>
            <div className="flex justify-end"><Button onClick={saveOrg} loading={busy} disabled={!orgLoaded}>Continue</Button></div>
          </CardContent>
        </Card>
      )}

      {step === 1 && (
        <Card className="p-6">
          <CardHeader><CardTitle>What kind of property is this?</CardTitle><CardSubtitle>The profile sets the terminology, default workflows, and modules for the property. One platform, configured for your operation.</CardSubtitle></CardHeader>
          <CardContent className="mt-4">
            <div className="grid gap-4 md:grid-cols-3">
              {(["hotel", "apartment", "office"] as ProfileCode[]).map((p) => {
                const selected = profile === p;
                return (
                  <button key={p} type="button" onClick={() => chooseProfile(p)} className={cn("rounded-[var(--radius-lg)] border-2 p-4 text-left transition-colors", selected ? "border-primary bg-primary-soft" : "border-border bg-surface hover:bg-surface-container-low")} aria-pressed={selected}>
                    <div className="flex items-center justify-between">
                      <Icon name={PROFILE_ICON[p]} size={28} className="text-primary" />
                      {selected && <Icon name="check_circle" size={22} className="text-primary" />}
                    </div>
                    <div className="mt-3 text-base font-bold text-on-surface">{PROFILE_LABEL[p]}</div>
                    <p className="mt-1 text-sm text-on-surface-variant">{PROFILE_DESC[p].blurb}</p>
                    <ul className="mt-3 space-y-1 text-xs text-on-surface-variant">
                      {PROFILE_DESC[p].bullets.map((b) => <li key={b} className="flex items-start gap-1.5"><Icon name="check" size={14} className="mt-0.5 text-primary" />{b}</li>)}
                    </ul>
                  </button>
                );
              })}
            </div>
            <div className="mt-6 flex justify-between">
              <Button variant="ghost" onClick={() => setStep(0)}>Back</Button>
              <Button onClick={() => setStep(2)} disabled={!profile}>Continue</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {step === 2 && profile && (
        <Card className="p-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">Create your property <Badge tone="info">{PROFILE_LABEL[profile]}</Badge></CardTitle>
            <CardSubtitle>Just the basics for now. Buildings, floors, and areas come next.</CardSubtitle>
          </CardHeader>
          <CardContent className="mt-4 grid gap-4 md:grid-cols-2">
            <Field label="Property name" required className="md:col-span-2"><Input autoFocus value={prop.name} onChange={(e) => setProp({ ...prop, name: e.target.value })} placeholder={profile === "hotel" ? "Grand Pangeran Hotel" : profile === "apartment" ? "Pangeran Residence" : "Graha Pangeran"} /></Field>
            <Field label="Address" className="md:col-span-2"><Input value={prop.address} onChange={(e) => setProp({ ...prop, address: e.target.value })} placeholder="Jl. Pangeran No. 1" /></Field>
            <Field label="City"><Input value={prop.city} onChange={(e) => setProp({ ...prop, city: e.target.value })} placeholder="Jakarta" /></Field>
            <Field label="Timezone" required>
              <NativeSelect value={prop.timezone} onChange={(e) => setProp({ ...prop, timezone: e.target.value })}>
                {TIMEZONES.map((z) => <option key={z} value={z}>{z}</option>)}
              </NativeSelect>
            </Field>
            <Field label="Contact phone" help="Shown to your team as the property contact."><Input value={prop.contact_phone} onChange={(e) => setProp({ ...prop, contact_phone: e.target.value })} placeholder="+62 21 000 0000" /></Field>
            <Field label="Contact email"><Input type="email" value={prop.contact_email} onChange={(e) => setProp({ ...prop, contact_email: e.target.value })} placeholder="ops@yourcompany.com" /></Field>
            <div className="flex justify-between md:col-span-2">
              <Button variant="ghost" onClick={() => setStep(1)}>Back</Button>
              <Button onClick={createProperty} loading={busy}>Create property</Button>
            </div>
          </CardContent>
        </Card>
      )}
    </>
  );
}

// ---------- Checklist (progressive onboarding, §28–§30) ----------
export function ChecklistView({ data, onChanged, compact }: { data: Onboarding; onChanged: () => void; compact?: boolean }) {
  const { can } = useAuth();
  const toast = useToast();
  const nav = useNavigate();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const pct = data.total ? Math.round((data.completed / data.total) * 100) : 0;
  const canAdmin = can("platform.organizations.update");

  const sample = async () => {
    if (!data.property_id) return;
    setBusy(true);
    try {
      await api("onboarding/sample-data", { body: { property_id: data.property_id } });
      track("sample_data_added");
      toast.success("Sample data added. Everything sample is labeled [Sample].");
      qc.invalidateQueries();
      onChanged();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(false);
    }
  };
  const dismiss = async () => {
    try {
      await api("onboarding/dismiss", { body: { dismissed: true } });
      qc.invalidateQueries({ queryKey: ["onboarding"] });
      onChanged();
    } catch (e) {
      toast.error(e);
    }
  };
  const activatedNote = useMemo(() => (data.activated ? "Your team has run a full workflow: request, work order, completion, and evidence." : "Run one request end to end to see how BuildingVision fits your team."), [data.activated]);

  useEffect(() => {
    if (data.completed === data.total && data.total > 0) track("onboarding_completed");
  }, [data.completed, data.total]);

  return (
    <div className={cn(!compact && "space-y-5")}>
      {!compact && (
        <PageHeader
          title={data.activated ? "You are up and running" : "Get started with BuildingVision"}
          subtitle={activatedNote}
          badges={data.trial && data.trial.status !== "none" && data.trial.status !== "converted" ? <Badge tone={data.trial.locked ? "error" : data.trial.status === "trial_ending_soon" ? "warning" : "info"}>{data.trial.locked ? "Trial ended" : `${data.trial.days_left} day(s) left in trial`}</Badge> : undefined}
          actions={data.trial && data.trial.status !== "converted" && data.trial.status !== "none" ? <Button variant="secondary" onClick={() => nav("/settings/plan")}>Choose a plan</Button> : undefined}
        />
      )}
      <Card className={cn(compact ? "p-4" : "p-6")}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="text-sm font-semibold text-on-surface">{compact ? "Setup checklist" : `Checklist for ${data.property_name ?? "your property"}`} <span className="text-on-surface-variant">({data.completed}/{data.total})</span></div>
            {data.profile && <div className="mt-0.5 text-xs text-on-surface-variant">Profile: {PROFILE_LABEL[data.profile]}{data.sample_data_added_at ? ` · Sample data added ${fmtDate(data.sample_data_added_at)}` : ""}</div>}
          </div>
          <div className="flex items-center gap-2">
            {canAdmin && !data.sample_data_added_at && data.property_id && <Button size="sm" variant="secondary" loading={busy} onClick={sample} icon="auto_awesome">Use sample property</Button>}
            {compact && <Link to="/onboarding" className="text-sm font-medium text-primary underline">Open onboarding</Link>}
            {compact && canAdmin && <Button size="sm" variant="ghost" onClick={dismiss} aria-label="Hide checklist"><Icon name="close" size={16} /></Button>}
          </div>
        </div>
        <div className="mt-3 h-2 w-full overflow-hidden rounded-full bg-surface-container"><div className="h-full rounded-full bg-primary transition-all" style={{ width: `${pct}%` }} /></div>
        <ul className={cn("mt-4 grid gap-2", compact ? "md:grid-cols-2 lg:grid-cols-4" : "md:grid-cols-2")}>
          {data.items.map((it) => (
            <li key={it.key}>
              <Link to={it.link} className={cn("flex items-start gap-3 rounded-[var(--radius-md)] border border-border p-3 transition-colors hover:bg-surface-container-low", it.done && "opacity-80")}>
                <Icon name={it.done ? "check_circle" : "radio_button_unchecked"} size={22} className={it.done ? "text-primary" : "text-on-surface-variant"} />
                <div className="min-w-0">
                  <div className={cn("text-sm font-medium text-on-surface", it.done && "line-through")}>{it.label}</div>
                  {!compact && <div className="mt-0.5 text-xs text-on-surface-variant">{it.hint}</div>}
                </div>
              </Link>
            </li>
          ))}
        </ul>
      </Card>
      {!compact && (
        <Card className="p-6">
          <CardHeader><CardTitle>Bring your team in</CardTitle><CardSubtitle>BuildingVision works best when the front line uses it too.</CardSubtitle></CardHeader>
          <CardContent className="mt-3 grid gap-3 md:grid-cols-2">
            <div className="rounded-[var(--radius-md)] border border-border p-4">
              <div className="flex items-center gap-2 font-semibold text-on-surface"><Icon name="smartphone" size={20} className="text-primary" /> Staff App</div>
              <p className="mt-1 text-sm text-on-surface-variant">Technicians, security officers, and housekeeping staff receive tasks, scan checkpoints, and upload photo evidence, even offline.</p>
              <Link to="/settings/users" className="mt-2 inline-block text-sm font-medium text-primary underline">Invite staff</Link>
            </div>
            <div className="rounded-[var(--radius-md)] border border-border p-4">
              <div className="flex items-center gap-2 font-semibold text-on-surface"><Icon name="apartment" size={20} className="text-primary" /> Tenant App</div>
              <p className="mt-1 text-sm text-on-surface-variant">{data.profile === "hotel" ? "Guests" : data.profile === "apartment" ? "Residents" : "Tenants"} report issues, follow progress, book facilities, and register visitors from their phone.</p>
              <Link to="/tenant-relation/tenant-users" className="mt-2 inline-block text-sm font-medium text-primary underline">Invite {data.profile === "hotel" ? "a guest" : data.profile === "apartment" ? "a resident" : "a tenant"}</Link>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
