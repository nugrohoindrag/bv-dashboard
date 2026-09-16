// /download (Website PRD §13–§14, §22–§23, §47): tautan aktif dari public API; fallback bila tidak ada / API gagal.
import { useEffect } from "react";
import { Seo } from "@/lib/head";
import { CtaBand, DownloadCards, FaqList, useApps } from "@/components/blocks";
import { PhoneMock } from "@/components/mocks";
import { Picture } from "@/components/Picture";
import { Icon } from "@/components/Icon";
import { Container, Eyebrow, Heading, Lead, Section } from "@/components/ui";
import { track } from "@/lib/analytics";

const STEPS = [
  { icon: "download", title: "Download", body: "Tap the download button. The file opens from BuildingVision's distribution folder on Google Drive." },
  { icon: "install_mobile", title: "Install", body: "Android may ask you to allow installs from this source. Accept, then open the app." },
  { icon: "login", title: "Log in", body: "Staff use the account created by their organization admin. Tenants register in the app or use the account from Tenant Relation." },
];

export default function DownloadPage() {
  const { apps, loading, error } = useApps();
  useEffect(() => {
    track("download_apps_viewed");
  }, []);
  return (
    <>
      <Seo title="Download BuildingVision Apps" description="Get the BuildingVision Staff App for building teams and the Tenant App for tenants, residents, and guests." path="/download" image="/images/devices-table-1440.webp" />
      <section className="bv-hero-bg">
        <Container className="grid items-center gap-10 py-16 sm:py-20 lg:grid-cols-12">
          <div className="lg:col-span-7">
            <Eyebrow>Download Apps</Eyebrow>
            <Heading as="h1">Download BuildingVision Apps</Heading>
            <Lead>Use BuildingVision wherever your team works. The Staff App for building staff and operational teams, the Tenant App for tenants, residents, and guests.</Lead>
          </div>
          <div className="flex justify-center gap-4 lg:col-span-5">
            <PhoneMock variant="staff" className="w-[200px]" />
            <PhoneMock variant="tenant" className="hidden w-[200px] sm:block" />
          </div>
        </Container>
      </section>
      <Section className="pt-0 sm:pt-0">
        <DownloadCards apps={apps} loading={loading} error={error} />
        <p className="mt-4 text-xs text-on-surface-variant">Downloads are distributed through Google Drive and updated by the BuildingVision team. Always install from this page. The Tenant App also works in the browser as a web app: ask your building management for the link.</p>
      </Section>
      <Section tone="muted">
        <div className="max-w-2xl"><Eyebrow>Install</Eyebrow><Heading>Three steps to get going</Heading></div>
        <div className="mt-10 grid gap-5 md:grid-cols-3">
          {STEPS.map((s) => <div key={s.title} className="rounded-[var(--radius-xl)] border border-border bg-surface p-6"><div className="mb-3 inline-flex h-11 w-11 items-center justify-center rounded-[var(--radius-lg)] bg-primary-soft text-primary"><Icon name={s.icon} size={24} /></div><h3 className="font-bold text-on-surface">{s.title}</h3><p className="mt-1 text-sm text-on-surface-variant">{s.body}</p></div>)}
        </div>
      </Section>
      <Section>
        <div className="grid items-center gap-10 lg:grid-cols-12">
          <div className="lg:col-span-6"><Picture image="site-workers" sizes="(min-width: 1024px) 560px, 100vw" /></div>
          <div className="lg:col-span-6"><Eyebrow>For teams</Eyebrow><Heading>Rolling out to your staff</Heading><Lead>Organization admins create staff accounts from the dashboard, assign roles per property, and share this page. Field staff log in once and get their work for the day, even offline.</Lead></div>
        </div>
      </Section>
      <FaqList tone="muted" title="Download questions" items={[
        { q: "Is there an iOS version?", a: "The Staff App is Android first. When an iOS build is available it appears on this page automatically. The Tenant App runs as a web app on both Android and iOS." },
        { q: "Why does the download open Google Drive?", a: "During the initial rollout, BuildingVision distributes app builds through a managed Google Drive folder. The link on this page always points to the current build." },
        { q: "The download button is missing. What now?", a: "It means a build is not published right now. Try again later or contact your building management or BuildingVision support." },
      ]} />
      <CtaBand title="Not a customer yet?" lead="Start a free trial and try the apps with your own team." source="download_cta" />
    </>
  );
}
