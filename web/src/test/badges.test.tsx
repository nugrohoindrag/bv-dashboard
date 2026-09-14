import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import "@/lib/i18n";
import { FlagBadges, PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { statusMap } from "@/lib/status-map";

describe("StatusBadge (kontrak status-map.yaml)", () => {
  it("merender label Indonesia untuk status work_order", () => {
    render(<StatusBadge objectType="work_order" status="in_progress" />);
    expect(screen.getByText(statusMap.work_order.in_progress.label_id)).toBeInTheDocument();
  });
  it("menampilkan status mentah bila tidak ada di peta", () => {
    render(<StatusBadge objectType="task" status="unknown_status" />);
    expect(screen.getByText(/unknown_status/)).toBeInTheDocument();
  });
  it("setiap object type P0 punya status awal 'new' (TD-001)", () => {
    for (const ot of ["task", "work_order", "service_request", "incident"] as const) expect(statusMap[ot].new).toBeDefined();
  });
});

describe("Priority & flag badges", () => {
  it("prioritas critical memakai semantic critical", () => {
    const { container } = render(<PriorityBadge priority="critical" />);
    expect(container.textContent).toMatch(/kritis|critical/i);
  });
  it("FlagBadges merender overdue dan sla_risk", () => {
    render(<FlagBadges flags={["overdue", "sla_risk"]} />);
    expect(screen.getByText(/overdue/i)).toBeInTheDocument();
    expect(screen.getByText(/sla risk/i)).toBeInTheDocument();
  });
});
