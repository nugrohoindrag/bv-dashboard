// Settings › Plan & Trial (Website PRD §31–§33): status trial, pilih plan (pembayaran online di-hold: sales menindaklanjuti), batalkan trial.
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, ConfirmDialog, Icon } from "@/components/ui/primitives";
import { AsyncState, useToast } from "@/components/bv/common";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDate } from "@/lib/format";
import { TRIAL_LABEL, track, useTrial, type Plan, type TrialInfo } from "@/lib/growth";
import { cn } from "@/lib/utils";

export default function PlanSection() {
  const q = useTrial();
  return <AsyncState query={q}>{(d) => <PlanView trial={d.trial} plans={d.plans} onChanged={() => q.refetch()} />}</AsyncState>;
}

function PlanView({ trial, plans, onChanged }: { trial: TrialInfo; plans: Plan[]; onChanged: () => void }) {
  const { can } = useAuth();
  const toast = useToast();
  const qc = useQueryClient();
  const [busy, setBusy] = useState<string | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const canEdit = can("platform.organizations.update");
  const onTrial = trial.status === "trial" || trial.status === "trial_ending_soon";

  const choose = async (plan: Plan) => {
    if (plan.cta === "contact_sales") {
      window.open("mailto:sales@buildingvision.id?subject=BuildingVision%20Enterprise", "_blank");
      return;
    }
    setBusy(plan.code);
    try {
      await api("trial/convert", { body: { plan_code: plan.code } });
      track("subscription_started", { plan: plan.code });
      toast.success(`You are on the ${plan.name} plan. Our team will confirm billing details with you.`);
      qc.invalidateQueries();
      onChanged();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const cancel = async () => {
    setBusy("cancel");
    try {
      await api("trial/cancel", { body: { reason: "user_cancelled" } });
      qc.invalidateQueries();
      onChanged();
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
      setCancelOpen(false);
    }
  };

  return (
    <div className="space-y-5">
      {trial.status !== "none" && (
        <Card className="p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-2 text-base font-bold text-on-surface">
                {TRIAL_LABEL[trial.status]}
                <Badge tone={trial.locked ? "error" : trial.status === "trial_ending_soon" ? "warning" : trial.status === "converted" ? "success" : "info"}>{trial.status.replace(/_/g, " ")}</Badge>
              </div>
              <p className="mt-1 text-sm text-on-surface-variant">
                {onTrial && trial.ends_at && <>Your free trial ends on <strong className="text-on-surface">{fmtDate(trial.ends_at)}</strong> ({trial.days_left} day(s) left). All features are unlocked until then.</>}
                {trial.status === "trial_expired" && <>Your trial ended{trial.ends_at ? ` on ${fmtDate(trial.ends_at)}` : ""}. Your data is safe. Choose a plan to keep making changes.</>}
                {trial.status === "cancelled" && <>You cancelled the trial{trial.cancelled_at ? ` on ${fmtDate(trial.cancelled_at)}` : ""}. Choose a plan any time to pick up where you left off.</>}
                {trial.status === "converted" && <>Thanks for choosing BuildingVision. Plan: <strong className="text-on-surface">{trial.plan_code}</strong>{trial.converted_at ? `, since ${fmtDate(trial.converted_at)}` : ""}.</>}
              </p>
            </div>
            {onTrial && canEdit && <Button variant="ghost" onClick={() => setCancelOpen(true)}>Cancel trial</Button>}
          </div>
        </Card>
      )}
      {trial.locked && <Alert variant="warning" title="Read-only mode">Viewing is still available, but creating or editing records is paused until a plan is chosen.</Alert>}
      <Alert variant="info" title="How billing works right now">Online payment is not switched on yet. When you choose a plan, our team confirms the details with you by email and activates billing manually. Nothing is charged automatically.</Alert>
      <div className="grid gap-5 lg:grid-cols-3">
        {plans.map((p) => {
          const current = trial.plan_code === p.code && trial.status === "converted";
          return (
            <Card key={p.code} className={cn("flex flex-col p-5", p.highlighted && "ring-2 ring-primary")}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">{p.name} {p.highlighted && <Badge tone="primary">Most popular</Badge>} {current && <Badge tone="success">Current</Badge>}</CardTitle>
                <CardSubtitle>{p.tagline}</CardSubtitle>
              </CardHeader>
              <CardContent className="mt-3 flex flex-1 flex-col">
                <div className="text-2xl font-extrabold text-on-surface">{p.price_label}</div>
                <div className="text-xs text-on-surface-variant">{p.period}</div>
                <ul className="mt-4 space-y-1.5 text-sm text-on-surface">
                  <li className="flex gap-2"><Icon name="domain" size={18} className="text-primary" />{p.properties}</li>
                  <li className="flex gap-2"><Icon name="group" size={18} className="text-primary" />{p.staff_limit}</li>
                  {p.includes.map((i) => <li key={i} className="flex gap-2"><Icon name="check" size={18} className="text-primary" />{i}</li>)}
                </ul>
                <div className="mt-auto pt-5">
                  <Button className="w-full" variant={p.highlighted ? "primary" : "secondary"} disabled={!canEdit || current || (trial.status === "converted")} loading={busy === p.code} onClick={() => choose(p)}>
                    {p.cta === "contact_sales" ? "Contact sales" : current ? "Current plan" : "Choose " + p.name}
                  </Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>
      <ConfirmDialog open={cancelOpen} onOpenChange={setCancelOpen} title="Cancel your trial?" description="Your workspace becomes read-only right away. Nothing is deleted, and you can choose a plan later to continue." confirmLabel="Cancel trial" destructive onConfirm={cancel} />
    </div>
  );
}
