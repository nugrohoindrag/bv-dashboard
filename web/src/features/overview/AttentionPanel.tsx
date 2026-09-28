// Attention Required + aksi cepat (DS §4.4; PRD P1 v2 §11; PRD P2 v2.1 P2-EMG-09): dipakai Dashboard domain
// (PRD P2 v2.1 §5.1/§6.1/§7.1); handler aksi di useAttentionActions agar perilakunya sama dengan daftar Overview.
import { useTranslation } from "react-i18next";
import { AttentionRequiredList } from "@/components/bv/cards";
import type { AttentionItem } from "@/api/types";
import { localizeAttention, useAttentionActions } from "./useAttentionActions";

export function AttentionPanel({ items, total, limit = 10, onSeeAll, seeAllTo, emptyText }: { items: AttentionItem[]; total?: number; limit?: number; onSeeAll?: () => void; seeAllTo?: string; emptyText?: string }) {
  const { t } = useTranslation();
  const { onAction, dialogs } = useAttentionActions();

  return (
    <>
      {items.length ? (
        <AttentionRequiredList items={items.map(localizeAttention)} total={total} limit={limit} onAction={onAction} onSeeAll={onSeeAll} seeAllTo={seeAllTo} />
      ) : (
        <p className="py-8 text-center text-sm text-on-surface-variant">{emptyText ?? t("empty.attention")}</p>
      )}
      {dialogs}
    </>
  );
}
