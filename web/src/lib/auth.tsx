// Auth context: login/logout, principal (permission per property), property switcher (DS §3.1), can() helper (TAD §9.2).
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, refreshToken, tokenStore } from "./api";

export interface PropertyScope {
  property_id: string | null;
  scope_location_id?: string | null; // scope Building/Tower (PRD P0 v2 §8.4)
  permissions: string[];
}
export interface Principal {
  id: string;
  full_name: string;
  organization_id: string;
  roles: string[];
  team_ids: string[];
  lead_team_ids: string[];
  permissions: string[];
  properties: PropertyScope[];
  is_internal_admin?: boolean; // role admin_internal pada organization internal (Website PRD §18)
  is_platform_admin?: boolean; // Platform Admin (PRD P0 v2 §6): registry seluruh organization
  vendor_id?: string | null; // user vendor (PRD P0 v2 §8.1)
}
export interface PropertyLite {
  id: string;
  name: string;
  code: string;
  details?: Record<string, unknown>;
}

interface AuthState {
  principal: Principal | null;
  loading: boolean;
  properties: PropertyLite[];
  propertyId: string | null; // null = semua properti (Property Manager / Org Admin pada Overview)
  setPropertyId: (id: string | null) => void;
  login: (identifier: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  /** Hapus sesi lokal tanpa memanggil server (mis. setelah POST /auth/logout-all). */
  clearSession: () => void;
  can: (perm: string, propertyId?: string | null) => boolean;
  refreshPrincipal: () => Promise<void>;
}

const Ctx = createContext<AuthState | null>(null);

function matchPerm(set: Set<string>, perm: string): boolean {
  if (set.has(perm) || set.has("*")) return true;
  const [m, o, a] = perm.split(".");
  return set.has(`${m}.*`) || set.has(`${m}.${o}.*`) || set.has(`${m}.*.${a}`);
}

const PROP_KEY = "bv.property_id";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [principal, setPrincipal] = useState<Principal | null>(null);
  const [loading, setLoading] = useState(true);
  const [properties, setProperties] = useState<PropertyLite[]>([]);
  const [propertyId, setPropertyIdState] = useState<string | null>(() => {
    try {
      return localStorage.getItem(PROP_KEY);
    } catch {
      return null;
    }
  });

  const loadSession = useCallback(async () => {
    try {
      const me = await api<{ principal: Principal }>("me/permissions");
      // /me/permissions mengembalikan principal flat
      const p = (me as unknown as Principal).id ? (me as unknown as Principal) : me.principal;
      setPrincipal(p);
      // admin_internal (organization internal) tidak punya property/permission property.view → 403 bukan berarti sesi gagal
      let props: { data: PropertyLite[] } = { data: [] };
      // tanpa property.locations.view (mis. Platform Admin / admin_internal) tidak perlu memanggil /properties (403)
      const canViewProps = p.permissions?.some((x) => x === "*" || x === "property.locations.view" || x === "property.*" || x === "property.locations.*");
      if (canViewProps) {
        try {
          props = await api<{ data: PropertyLite[] }>("properties");
        } catch {
          props = { data: [] };
        }
      }
      setProperties(props.data);
      setPropertyIdState((cur) => {
        if (cur && props.data.some((x) => x.id === cur)) return cur;
        const canAll = p.properties.some((s) => s.property_id === null);
        const next = canAll ? null : props.data[0]?.id ?? null;
        try {
          if (next) localStorage.setItem(PROP_KEY, next);
          else localStorage.removeItem(PROP_KEY);
        } catch {
          /* ignore */
        }
        return next;
      });
    } catch {
      setPrincipal(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // coba refresh dari cookie saat mount
    (async () => {
      try {
        if (await refreshToken()) {
          await loadSession();
          return;
        }
      } catch {
        /* offline / no session */
      }
      setLoading(false);
    })();
  }, [loadSession]);

  const login = useCallback(
    async (identifier: string, password: string) => {
      const res = await api<{ access_token: string; user: Principal }>("auth/login", { body: { identifier, password, client: "web" } });
      tokenStore.set(res.access_token);
      setLoading(true);
      await loadSession();
    },
    [loadSession],
  );

  const logout = useCallback(async () => {
    try {
      await api("auth/logout", { method: "POST", body: {} });
    } catch {
      /* ignore */
    }
    tokenStore.set(null);
    setPrincipal(null);
  }, []);

  const clearSession = useCallback(() => {
    tokenStore.set(null);
    setPrincipal(null);
  }, []);

  const setPropertyId = useCallback((id: string | null) => {
    setPropertyIdState(id);
    try {
      if (id) localStorage.setItem(PROP_KEY, id);
      else localStorage.removeItem(PROP_KEY);
    } catch {
      /* ignore */
    }
  }, []);

  const can = useCallback(
    (perm: string, pid?: string | null) => {
      if (!principal) return false;
      const target = pid === undefined ? propertyId : pid;
      for (const s of principal.properties) {
        if (s.property_id !== null && target && s.property_id !== target) continue;
        if (matchPerm(new Set(s.permissions), perm)) return true;
      }
      return false;
    },
    [principal, propertyId],
  );

  const value = useMemo<AuthState>(
    () => ({ principal, loading, properties, propertyId, setPropertyId, login, logout, clearSession, can, refreshPrincipal: loadSession }),
    [principal, loading, properties, propertyId, setPropertyId, login, logout, clearSession, can, loadSession],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth di luar AuthProvider");
  return v;
}
