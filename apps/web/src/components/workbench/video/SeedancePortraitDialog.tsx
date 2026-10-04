"use client";

/* eslint-disable @next/next/no-img-element */

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { CheckCircle2, Loader2, RefreshCw, ScanFace, Upload, UserRound, X } from "lucide-react";
import { api, uploadAsset } from "@/lib/api";
import { useI18n } from "@/i18n/I18nProvider";

type PortraitGroup = { group_id: string; project_name: string; created_at: string };
type PortraitAsset = { id: string; name?: string; url?: string; group_id: string; asset_type: string; status: string; created_at?: string };
type PortraitLibrary = { configured: boolean; project_name: string; groups: PortraitGroup[]; items: PortraitAsset[] };

export function SeedancePortraitDialog({
  open,
  selectedId,
  onClose,
  onSelect,
}: {
  open: boolean;
  selectedId?: string;
  onClose: () => void;
  onSelect: (id: string, type: "image" | "video") => void;
}) {
  const { ts } = useI18n();
  const [library, setLibrary] = useState<PortraitLibrary | null>(null);
  const [groupId, setGroupId] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [manualId, setManualId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await api<PortraitLibrary>("/api/seedance/portrait-assets");
      setLibrary(result);
      setGroupId((current) => current || result.groups?.[0]?.group_id || "");
    } catch (err) {
      setError(err instanceof Error ? err.message : ts("读取真人素材失败"));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    if (!open) return;
    setManualId(selectedId?.replace(/^asset:\/\//, "") || "");
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const startVerification = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ session_id: string; h5_link: string }>("/api/seedance/portrait-sessions", { method: "POST" });
      setSessionId(result.session_id);
      const popup = window.open(result.h5_link, "seedance-portrait-verification", "popup,width=520,height=760");
      if (!popup) setError(ts("浏览器拦截了认证窗口，请允许弹窗后重试"));
    } catch (err) {
      setError(err instanceof Error ? err.message : ts("创建真人认证失败"));
    } finally {
      setBusy(false);
    }
  };

  const completeVerification = async () => {
    if (!sessionId) return;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ group_id: string }>(`/api/seedance/portrait-sessions/${encodeURIComponent(sessionId)}/complete`, { method: "POST" });
      setGroupId(result.group_id);
      setSessionId("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : ts("尚未取得认证结果"));
      setBusy(false);
    }
  };

  const upload = async (file?: File) => {
    if (!file || !groupId) return;
    const kind = file.type.startsWith("video/") ? "video" : "image";
    setBusy(true);
    setError("");
    try {
      const local = await uploadAsset(file, { name: file.name, kind, asset_type: "role" });
      await api("/api/seedance/portrait-assets", {
        method: "POST",
        body: JSON.stringify({ group_id: groupId, local_asset_id: local.public_id, name: file.name, asset_type: kind === "video" ? "Video" : "Image" }),
      });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : ts("上传真人素材失败"));
      setBusy(false);
    }
  };

  const importManual = async () => {
    const assetId = manualId.trim().replace(/^asset:\/\//, "");
    if (!assetId) return;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ id: string; asset_type: string }>("/api/seedance/portrait-assets/import", {
        method: "POST",
        body: JSON.stringify({ asset_id: assetId }),
      });
      onSelect(result.id, result.asset_type.toLowerCase() === "video" ? "video" : "image");
    } catch (err) {
      setError(err instanceof Error ? err.message : ts("导入真人素材失败"));
      setBusy(false);
    }
  };

  if (!open || typeof document === "undefined") return null;
  const items = (library?.items || []).filter((item) => !groupId || item.group_id === groupId);
  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/55 p-3 backdrop-blur-sm" onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label={ts("Seedance 真人素材库")} className="flex max-h-[min(720px,calc(100dvh-1.5rem))] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-2xl dark:border-white/10 dark:bg-gray-900" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-white/10">
          <div><h2 className="font-bold text-gray-900 dark:text-white">{ts("Seedance 真人素材库")}</h2><p className="mt-1 text-xs text-gray-400">{ts("每位真人需先完成一次 H5 活体认证，再上传同一人物的图片或视频")}</p></div>
          <button type="button" aria-label={ts("关闭")} onClick={onClose} className="grid h-8 w-8 place-items-center rounded-lg bg-gray-100 text-gray-500 dark:bg-white/10"><X size={16}/></button>
        </header>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4 sm:p-5">
          {error && <div className="rounded-xl bg-red-50 px-3 py-2 text-xs text-red-600 dark:bg-red-400/10 dark:text-red-300">{error}</div>}
          <div className="rounded-xl border border-cyan-100 bg-cyan-50/60 p-3 dark:border-cyan-400/20 dark:bg-cyan-400/10">
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" disabled={busy} onClick={startVerification} className="inline-flex h-9 items-center gap-2 rounded-xl bg-cyan-600 px-3 text-sm font-semibold text-white disabled:opacity-50"><ScanFace size={16}/>{ts("开始真人认证")}</button>
              {sessionId && <button type="button" disabled={busy} onClick={completeVerification} className="inline-flex h-9 items-center gap-2 rounded-xl border border-cyan-200 bg-white px-3 text-sm font-medium text-cyan-700 disabled:opacity-50 dark:bg-gray-900"><CheckCircle2 size={16}/>{ts("我已完成，检查结果")}</button>}
              <button type="button" disabled={busy} onClick={() => void load()} className="ml-auto grid h-9 w-9 place-items-center rounded-xl border border-cyan-200 bg-white text-cyan-700 disabled:opacity-50 dark:bg-gray-900" title={ts("刷新")}><RefreshCw size={15}/></button>
            </div>
            {sessionId && <p className="mt-2 text-xs text-cyan-800 dark:text-cyan-200">{ts("请在弹出的火山页面完成活体认证，然后返回点击“我已完成，检查结果”。")}</p>}
          </div>

          {!!library?.groups?.length && <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <label className="min-w-0 flex-1 text-xs text-gray-500">{ts("真人素材组")}<select value={groupId} onChange={(event) => setGroupId(event.target.value)} className="mt-1 h-10 w-full rounded-xl border border-gray-200 bg-white px-3 text-sm outline-none dark:border-white/10 dark:bg-gray-950">{library.groups.map((group, index) => <option key={group.group_id} value={group.group_id}>{ts("真人形象")} {library.groups.length - index} · {group.group_id}</option>)}</select></label>
            <label className="inline-flex h-10 cursor-pointer items-center justify-center gap-2 rounded-xl bg-gray-950 px-4 text-sm font-semibold text-white dark:bg-white dark:text-gray-950"><Upload size={15}/>{busy ? ts("处理中…") : ts("上传同一人物素材")}<input type="file" className="hidden" disabled={busy} accept="image/png,image/jpeg,image/webp,video/mp4,video/quicktime" onChange={(event) => { void upload(event.target.files?.[0]); event.target.value = ""; }}/></label>
          </div>}

          <div className="min-h-40 rounded-xl border border-gray-100 bg-gray-50 p-2 dark:border-white/10 dark:bg-white/5">
            {busy && !library ? <div className="flex h-40 items-center justify-center gap-2 text-sm text-gray-400"><Loader2 size={17} className="animate-spin"/>{ts("加载中…")}</div> : items.length ? <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
              {items.map((item) => {
                const active = item.status === "Active";
                const selected = selectedId?.replace(/^asset:\/\//, "") === item.id;
                return <button key={item.id} type="button" disabled={!active} onClick={() => onSelect(item.id, item.asset_type.toLowerCase() === "video" ? "video" : "image")} className={`overflow-hidden rounded-xl border bg-white text-left disabled:cursor-not-allowed dark:bg-gray-900 ${selected ? "border-cyan-500 ring-2 ring-cyan-200" : "border-gray-200 dark:border-white/10"}`}>
                  <div className="aspect-square bg-gray-100 dark:bg-white/5">{item.asset_type === "Video" && item.url ? <video src={item.url} muted preload="metadata" className="h-full w-full object-cover"/> : item.url ? <>{/* eslint-disable-next-line @next/next/no-img-element */}<img src={item.url} alt="" className="h-full w-full object-cover"/></> : <div className="grid h-full place-items-center text-gray-300"><UserRound size={28}/></div>}</div>
                  <div className="p-2"><div className="truncate text-xs font-medium">{item.name || item.id}</div><div className={`mt-1 text-[10px] ${active ? "text-emerald-600" : item.status === "Failed" ? "text-red-500" : "text-amber-500"}`}>{active ? ts("可使用") : item.status === "Failed" ? ts("处理失败") : ts("处理中")}</div></div>
                </button>;
              })}
            </div> : <div className="flex h-40 flex-col items-center justify-center text-center text-sm text-gray-400"><UserRound size={30} className="mb-2 opacity-50"/><span>{library?.groups?.length ? ts("该真人形象还没有素材") : ts("请先完成真人认证")}</span></div>}
          </div>

          <div className="rounded-xl border border-gray-100 p-3 dark:border-white/10">
            <div className="mb-2 text-xs font-medium text-gray-500">{ts("已有 asset_id？可直接填写")}</div>
            <div className="flex gap-2"><input value={manualId} onChange={(event) => setManualId(event.target.value)} placeholder="asset-..." className="h-9 min-w-0 flex-1 rounded-lg border border-gray-200 bg-transparent px-3 text-sm outline-none dark:border-white/10"/><button type="button" disabled={!manualId.trim() || busy} onClick={() => void importManual()} className="h-9 rounded-lg bg-gray-100 px-3 text-sm font-medium disabled:opacity-40 dark:bg-white/10">{ts("导入")}</button></div>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
