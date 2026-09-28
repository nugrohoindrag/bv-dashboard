// Api client: 401 TOKEN_STALE → refresh transparan + ulang sekali (PRD P0 v2 §24.1); pelaporan client error (§24.4).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, tokenStore } from "@/lib/api";
import { reportClientError, resetClientErrorLimiter } from "@/lib/client-errors";

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("api client TOKEN_STALE", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    tokenStore.set("old-token");
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    tokenStore.set(null);
  });
  it("refresh token lalu mengulang permintaan dengan token baru", async () => {
    fetchMock
      .mockResolvedValueOnce(json(401, { type: "about:blank", title: "Unauthorized", status: 401, code: "TOKEN_STALE" }))
      .mockResolvedValueOnce(json(200, { access_token: "new-token" }))
      .mockResolvedValueOnce(json(200, { data: [{ id: "s1" }] }));
    const r = await api<{ data: { id: string }[] }>("me/sessions");
    expect(r.data[0].id).toBe("s1");
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls[1][0]).toBe("/api/v1/auth/refresh");
    const retryHeaders = fetchMock.mock.calls[2][1].headers as Record<string, string>;
    expect(retryHeaders.Authorization).toBe("Bearer new-token");
  });
  it("tidak berulang tanpa henti: 401 kedua dilempar sebagai ApiError", async () => {
    fetchMock
      .mockResolvedValueOnce(json(401, { status: 401, code: "TOKEN_STALE", title: "x", type: "" }))
      .mockResolvedValueOnce(json(200, { access_token: "t2" }))
      .mockResolvedValueOnce(json(401, { status: 401, code: "TOKEN_INVALID", title: "Unauthorized", type: "" }));
    await expect(api("me")).rejects.toMatchObject({ problem: { code: "TOKEN_INVALID" } });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });
});

describe("reportClientError", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    resetClientErrorLimiter();
    fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    tokenStore.set(null);
  });
  it("tidak mengirim saat belum login", () => {
    tokenStore.set(null);
    expect(reportClientError(new Error("boom"))).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("mengirim payload web lalu membatasi 5/menit", () => {
    tokenStore.set("tok");
    for (let i = 0; i < 7; i++) reportClientError(new Error("boom " + i), { route: "/x" });
    expect(fetchMock).toHaveBeenCalledTimes(5);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/client-errors");
    const body = JSON.parse(init.body as string);
    expect(body).toMatchObject({ message: "boom 0", route: "/x", source: "web" });
  });
});
