"use client";

import { useState } from "react";
import { ArrowRight, Film, Music2, Plus, UserRound, X } from "lucide-react";
import type { VideoMediaItem, VideoMediaState, VideoRuntimeConfig, VideoPromptReference } from "@starai/shared-types";
import { videoReferenceCapacity, videoReferenceToken } from "@starai/shared-types";
import { uploadAsset } from "@/lib/api";
import { useI18n } from "@/i18n/I18nProvider";
import { SeedancePortraitDialog } from "./SeedancePortraitDialog";

const IMAGE_ACCEPT = "image/png,image/jpeg,image/webp,image/gif,image/bmp,image/tiff";
const VIDEO_ACCEPT = "video/mp4,video/quicktime";
const AUDIO_ACCEPT = "audio/mpeg,audio/wav,audio/x-wav";

function readMediaDuration(file: File, kind: "video" | "audio"): Promise<number | undefined> {
  return new Promise((resolve) => {
    const element = document.createElement(kind);
    const objectURL = URL.createObjectURL(file);
    const finish = (value?: number) => {
      URL.revokeObjectURL(objectURL);
      resolve(value && Number.isFinite(value) && value > 0 ? value : undefined);
    };
    element.preload = "metadata";
    element.onloadedmetadata = () => finish(element.duration);
    element.onerror = () => finish();
    element.src = objectURL;
  });
}

function EmptyUploadBox({
  label,
  onUpload,
  uploading,
  tilt,
  compact,
  multiple,
  accept = IMAGE_ACCEPT,
}: {
  label: string;
  onUpload: (files: FileList | null) => void;
  uploading?: boolean;
  tilt?: boolean;
  compact?: boolean;
  multiple?: boolean;
  accept?: string;
}) {
  return (
    <label
      className={`relative flex shrink-0 cursor-pointer flex-col items-center justify-center border border-dashed border-gray-200 bg-white shadow-sm transition hover:border-primary/40 hover:bg-primary/5 dark:border-white/10 dark:bg-white/5 dark:hover:bg-primary/10 ${
        compact ? "h-14 w-16 gap-0.5 rounded-xl" : "h-16 w-20 gap-1 rounded-2xl"
      } ${
        tilt ? "max-lg:rotate-0 lg:rotate-[-8deg]" : ""
      }`}
    >
      <Plus size={18} className="text-gray-400 dark:text-gray-300" />
      <span className="text-[10px] text-gray-400 dark:text-gray-300 text-center leading-tight px-1">{label}</span>
      <input
        type="file"
        accept={accept}
        multiple={multiple}
        className="hidden"
        disabled={uploading}
        onChange={(e) => {
          onUpload(e.target.files);
          e.target.value = "";
        }}
      />
    </label>
  );
}

function AddMoreButton({
  onUpload,
  uploading,
  multiple,
  accept = IMAGE_ACCEPT,
}: {
  onUpload: (files: FileList | null) => void;
  uploading?: boolean;
  multiple?: boolean;
  accept?: string;
}) {
  return (
    <label className="w-9 h-9 rounded-full border border-dashed border-gray-200 bg-white text-gray-400 flex items-center justify-center cursor-pointer hover:border-primary/40 hover:text-primary transition shrink-0 dark:border-white/10 dark:bg-white/5 dark:text-gray-300">
      <Plus size={16} />
      <input
        type="file"
        accept={accept}
        multiple={multiple}
        className="hidden"
        disabled={uploading}
        onChange={(e) => {
          onUpload(e.target.files);
          e.target.value = "";
        }}
      />
    </label>
  );
}

function FilledImageCard({
  image,
  badge,
  onRemove,
  compact,
}: {
  image: VideoMediaItem;
  badge?: string;
  onRemove?: () => void;
  compact?: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className={`group/img relative shrink-0 overflow-hidden border-2 border-white bg-gray-100 shadow-lg ${compact ? "h-14 w-14 rounded-xl" : "h-16 w-16 rounded-2xl"}`}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={image.url} alt={image.name} loading="lazy" decoding="async" className="w-full h-full object-cover" />
      {badge ? (
        <span className="pointer-events-none absolute left-1 top-1 px-1.5 py-0.5 rounded-md bg-black/55 text-white text-[10px]">
          {badge}
        </span>
      ) : null}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="absolute right-0.5 top-0.5 w-5 h-5 rounded-full bg-black/70 text-white flex items-center justify-center opacity-0 group-hover/img:opacity-100 transition"
          title={t("common.remove")}
        >
          <X size={12} />
        </button>
      )}
      <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-black/70 px-1.5 py-1 text-[10px] text-white opacity-0 group-hover/img:opacity-100 transition whitespace-nowrap truncate">
        {image.name}
      </div>
    </div>
  );
}

function FilledFileCard({
  item,
  kind,
  badge,
  onRemove,
}: {
  item: VideoMediaItem;
  kind: "video" | "audio";
  badge?: string;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const Icon = kind === "video" ? Film : Music2;
  return (
    <div className="group/file relative flex h-14 w-[4.5rem] shrink-0 flex-col items-center justify-center gap-0.5 overflow-hidden rounded-xl border border-gray-200 bg-white px-1.5 shadow-sm dark:border-white/10 dark:bg-white/5">
      <Icon size={18} className="text-primary" />
      <span className="w-full truncate text-center text-[10px] text-gray-500 dark:text-gray-300">{item.name}</span>
      {badge && <span className="rounded bg-primary/10 px-1 text-[10px] text-primary">{badge}</span>}
      <button
        type="button"
        onClick={onRemove}
        className="absolute right-1 top-1 flex h-5 w-5 items-center justify-center rounded-full bg-black/70 text-white opacity-0 transition group-hover/file:opacity-100"
        title={t("common.remove")}
      >
        <X size={12} />
      </button>
    </div>
  );
}

function ReferenceImageStack({
  images,
  references,
  max,
  uploading,
  onUpload,
  onRemove,
  compact,
}: {
  images: VideoMediaItem[];
  references?: VideoPromptReference[];
  max: number;
  uploading?: boolean;
  onUpload: (files: FileList | null) => void;
  onRemove: (index: number) => void;
  compact?: boolean;
}) {
  const { t, locale } = useI18n();
  const canAdd = images.length < max;

  if (images.length === 0) {
    if (max <= 0) return null;
    return (
      <EmptyUploadBox label={`${t("video.referenceImage")} 0/${max}`} uploading={uploading} tilt={!compact} compact={compact} multiple={max > 1} onUpload={onUpload} />
    );
  }

  return (
    <div className={compact ? "flex min-h-14 max-w-full flex-wrap items-center gap-1.5" : "scroll-x-only flex h-16 w-full shrink-0 flex-nowrap items-center gap-2"}>
      {images.map((img, i) => {
        const matches = references?.filter(item => item.kind === "image" && item.url === img.url);
        const reference = matches?.[images.slice(0, i).filter(item => item.url === img.url).length] || matches?.[0];
        return <FilledImageCard key={`${img.url}-${i}`} image={img} badge={reference ? videoReferenceToken(reference, locale).slice(1) : undefined} compact={compact} onRemove={() => onRemove(i)} />;
      })}
      {canAdd && <AddMoreButton uploading={uploading} multiple onUpload={onUpload} />}
    </div>
  );
}

function FrameSlot({
  label,
  image,
  uploading,
  onUpload,
  onRemove,
}: {
  label: string;
  image: VideoMediaItem | null;
  uploading?: boolean;
  onUpload: (files: FileList | null) => void;
  onRemove?: () => void;
}) {
  if (image) {
    return <FilledImageCard image={image} badge={label} onRemove={onRemove} />;
  }
  return <EmptyUploadBox label={label} uploading={uploading} onUpload={onUpload} />;
}

export function VideoUploadArea({
  config,
  references,
  media,
  onChange,
  mode,
  showBudgetNotice = true,
  portraitAssetId,
  portraitAssetType,
  onPortraitAssetIdChange,
  onPortraitAssetTypeChange,
  draftTaskId,
  onDraftTaskIdChange,
}: {
  config: VideoRuntimeConfig;
  references?: VideoPromptReference[];
  media: VideoMediaState;
  onChange: (next: VideoMediaState) => void;
  mode?: string;
  showBudgetNotice?: boolean;
  portraitAssetId?: string;
  portraitAssetType?: "image" | "video";
  onPortraitAssetIdChange?: (value: string) => void;
  onPortraitAssetTypeChange?: (value: "image" | "video") => void;
  draftTaskId?: string;
  onDraftTaskIdChange?: (value: string) => void;
}) {
  const { t, locale } = useI18n();
  const referenceLabel = (url?: string, kind?: VideoPromptReference["kind"], occurrence = 0) => {
    const matches = references?.filter(item => item.url === url && (!kind || item.kind === kind));
    const reference = matches?.[occurrence] || matches?.[0];
    return reference ? videoReferenceToken(reference, locale).slice(1) : undefined;
  };
  const [uploading, setUploading] = useState(false);
  const [portraitLibraryOpen, setPortraitLibraryOpen] = useState(false);
  const profile = config.upload_profile || "single_ref";
  const referenceBudget = videoReferenceCapacity(config, media, mode);
  const budgetNotice = !showBudgetNotice || referenceBudget.limit === undefined || ["image", "video", "audio"].includes(mode || "") ? null : (
    <span role="status" className="w-full text-[11px] text-gray-500 dark:text-gray-400">
      {t("video.materialBudget", { total: referenceBudget.total, limit: referenceBudget.limit, remaining: referenceBudget.remaining })}
    </span>
  );
  const exceedsBudget = (count: number, room: number) => {
    if (referenceBudget.limit === undefined || count <= room) return false;
    alert(t("video.materialRemaining", { remaining: Math.max(0, room) }));
    return true;
  };

  const uploadOne = async (files: FileList | null, apply: (item: VideoMediaItem) => void) => {
    const f = files?.[0];
    if (!f) return;
    setUploading(true);
    try {
      const asset = await uploadAsset(f, { name: f.name, kind: "image", asset_type: "prop" });
      apply({ url: asset.url, name: asset.name || f.name, public_id: asset.public_id });
    } catch (err) {
      alert(err instanceof Error ? err.message : t("asset.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  const uploadFiles = async (
    files: FileList | null,
    kind: "video" | "audio",
    current: VideoMediaItem[],
    max: number,
    apply: (items: VideoMediaItem[]) => void
  ) => {
    if (!files?.length) return;
    const room = Math.min(max - current.length, referenceBudget.remaining);
    if (exceedsBudget(files.length, room)) return;
    if (room <= 0) return;
    setUploading(true);
    try {
      const next: VideoMediaItem[] = [];
      for (const f of Array.from(files).slice(0, room)) {
        const durationSeconds = await readMediaDuration(f, kind);
        const asset = await uploadAsset(f, { name: f.name, kind, asset_type: "prop" });
        next.push({
          url: asset.url,
          name: asset.name || f.name,
          public_id: asset.public_id,
          duration_seconds: durationSeconds,
        });
      }
      apply([...current, ...next]);
    } catch (err) {
      alert(err instanceof Error ? err.message : t("asset.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  const uploadMany = async (files: FileList | null, max: number) => {
    if (!files?.length) return;
    const room = Math.min(max - media.reference_images.length, referenceBudget.remaining);
    if (exceedsBudget(files.length, room)) return;
    if (room <= 0) {
      alert(t("video.maxReferenceImages", { max }));
      return;
    }
    setUploading(true);
    try {
      const next: VideoMediaItem[] = [];
      for (const f of Array.from(files).slice(0, room)) {
        const asset = await uploadAsset(f, { name: f.name, kind: "image", asset_type: "prop" });
        next.push({ url: asset.url, name: asset.name || f.name, public_id: asset.public_id });
      }
      onChange({ ...media, reference_images: [...media.reference_images, ...next] });
    } catch (err) {
      alert(err instanceof Error ? err.message : t("asset.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  const refMax = () => {
    if (profile === "frame_pair") return config.reference_images?.max ?? 4;
    return config.max_reference_images ?? 1;
  };

  const removeRef = (index: number) => {
    onChange({ ...media, reference_images: media.reference_images.filter((_, idx) => idx !== index) });
  };

  if (profile === "veo_reference" || profile === "omni_reference") {
    const activeMode = mode || "text";
    if (activeMode === "text") return null;
    const profileLimit = profile === "omni_reference" ? 7 : 3;
    const max = Math.min(profileLimit, config.reference_images?.max ?? config.max_reference_images ?? profileLimit);
    return (
      <ReferenceImageStack references={references}
        images={media.reference_images}
        max={max}
        uploading={uploading}
        onUpload={(files) => uploadMany(files, max)}
        onRemove={removeRef}
        compact
      />
    );
  }

  if (mode === "text" && ["multi_ref", "single_ref", "none"].includes(profile)) return null;
  if (profile === "minimax_h3" || profile === "gateway_reference" || profile === "aliyun_multimodal" || profile.startsWith("aliyun_happyhorse_") || (profile === "seedance_2" && ["first_frame", "last_frame", "first_last", "reference"].includes(mode || "")) || (["multi_ref", "single_ref"].includes(profile) && ["first_frame", "first_last"].includes(mode || ""))) {
    const profileMode = profile === "aliyun_happyhorse_first_frame" ? "first_frame" : profile === "aliyun_happyhorse_reference" || profile === "aliyun_happyhorse_edit" ? "reference" : "text";
    const activeMode = mode || profileMode;
    if (activeMode === "text") return null;
    if (activeMode === "first_frame" || activeMode === "last_frame" || activeMode === "first_last") {
      const showFirst = activeMode === "first_frame" || activeMode === "first_last";
      const showLast = activeMode === "last_frame" || activeMode === "first_last";
      return (
        <div className="flex min-h-16 w-fit max-w-full flex-nowrap items-center gap-2">
          {showFirst && (
            <FrameSlot
              label={[t("video.firstFrame"), referenceLabel(media.first_frame?.url)].filter(Boolean).join(" · ")}
              image={media.first_frame}
              uploading={uploading}
              onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, first_frame: item }))}
              onRemove={() => onChange({ ...media, first_frame: null })}
            />
          )}
          {showFirst && showLast && <ArrowRight size={15} className="shrink-0 text-gray-300" />}
          {showLast && (
            <FrameSlot
              label={[t("video.lastFrame"), referenceLabel(media.last_frame?.url, "image", showFirst && media.first_frame?.url === media.last_frame?.url ? 1 : 0)].filter(Boolean).join(" · ")}
              image={media.last_frame}
              uploading={uploading}
              onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, last_frame: item }))}
              onRemove={() => onChange({ ...media, last_frame: null })}
            />
          )}
        </div>
      );
    }
    if (profile === "aliyun_happyhorse_reference") {
      return (
        <ReferenceImageStack references={references}
          images={media.reference_images}
          max={referenceBudget.reference_images}
          uploading={uploading}
          onUpload={(files) => uploadMany(files, referenceBudget.reference_images)}
          onRemove={removeRef}
          compact
        />
      );
    }
    const uses = (kind: string) => activeMode === "reference" || activeMode.split("_").includes(kind);
    const showImages = uses("image") && (referenceBudget.reference_images > 0 || media.reference_images.length > 0);
    const showVideos = uses("video") && (referenceBudget.reference_videos > 0 || media.reference_videos.length > 0);
    const showAudios = uses("audio") && profile !== "aliyun_happyhorse_edit" && (referenceBudget.reference_audios > 0 || media.reference_audios.length > 0);
    return (
      <div className="flex min-h-14 w-fit max-w-full flex-wrap items-center gap-1.5">
        {budgetNotice}
        {showImages && <ReferenceImageStack references={references}
          images={media.reference_images}
          max={referenceBudget.reference_images}
          uploading={uploading}
          onUpload={(files) => uploadMany(files, referenceBudget.reference_images)}
          onRemove={removeRef}
          compact
        />}
        {showImages && showVideos && <ArrowRight size={13} className="shrink-0 text-gray-300" />}
        {showVideos && <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-1.5">
          {media.reference_videos.map((item, index) => (
            <FilledFileCard
              key={`${item.url}-${index}`}
              item={item}
              kind="video" badge={referenceLabel(item.url, "video", media.reference_videos.slice(0, index).filter(previous => previous.url === item.url).length)}
              onRemove={() => onChange({ ...media, reference_videos: media.reference_videos.filter((_, i) => i !== index) })}
            />
          ))}
          {media.reference_videos.length < (referenceBudget.reference_videos) && (
            <EmptyUploadBox
              label={`${t("video.referenceVideo")} ${media.reference_videos.length}/${referenceBudget.reference_videos}`}
              compact
              accept={VIDEO_ACCEPT}
              uploading={uploading}
              onUpload={(files) =>
                uploadFiles(files, "video", media.reference_videos, referenceBudget.reference_videos, (items) =>
                  onChange({ ...media, reference_videos: items })
                )
              }
            />
          )}
        </div>}
        {(showImages || showVideos) && showAudios && <ArrowRight size={13} className="shrink-0 text-gray-300" />}
        {showAudios && <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-1.5">
          {media.reference_audios.map((item, index) => (
            <FilledFileCard
              key={`${item.url}-${index}`}
              item={item}
              kind="audio" badge={referenceLabel(item.url, "audio", media.reference_audios.slice(0, index).filter(previous => previous.url === item.url).length)}
              onRemove={() => onChange({ ...media, reference_audios: media.reference_audios.filter((_, i) => i !== index) })}
            />
          ))}
          {media.reference_audios.length < (referenceBudget.reference_audios) && (
            <EmptyUploadBox
              label={`${t("video.referenceAudio")} ${media.reference_audios.length}/${referenceBudget.reference_audios}`}
              compact
              accept={AUDIO_ACCEPT}
              uploading={uploading}
              onUpload={(files) =>
                uploadFiles(files, "audio", media.reference_audios, referenceBudget.reference_audios, (items) =>
                  onChange({ ...media, reference_audios: items })
                )
              }
            />
          )}
        </div>}
      </div>
    );
  }

  if (profile === "seedance_2") {
    const activeMode = mode || "text";
    const imageModes = new Set(["image", "image_audio", "image_video", "image_video_audio"]);
    const videoModes = new Set(["video", "video_audio", "image_video", "image_video_audio"]);
    const audioModes = new Set(["audio", "image_audio", "video_audio", "image_video_audio"]);
    const showImages = imageModes.has(activeMode);
    const showVideos = videoModes.has(activeMode);
    const showAudios = audioModes.has(activeMode);
    const showPortrait = (showImages || showVideos) && !!onPortraitAssetIdChange;
    if (activeMode === "text") return null;
    if (activeMode === "draft_task") {
      return (
        <div className="flex min-h-16 w-full items-center">
          <div className="flex h-16 w-full min-w-56 items-center gap-2 rounded-2xl border border-dashed border-violet-300 bg-violet-50/70 px-3 dark:border-violet-400/30 dark:bg-violet-400/10">
            <Film size={18} className="shrink-0 text-violet-500" />
            <div className="min-w-0 flex-1">
              <div className="mb-1 text-[10px] font-medium text-violet-600 dark:text-violet-300">{t("video.draftTask")}</div>
              <input
                value={draftTaskId || ""}
                onChange={(e) => onDraftTaskIdChange?.(e.target.value)}
                placeholder="cgt-..."
                className="h-7 w-full rounded-lg border border-violet-200 bg-white px-2 text-xs outline-none focus:border-violet-400 dark:border-white/10 dark:bg-white/5"
              />
            </div>
          </div>
        </div>
      );
    }
    return (
      <>
      <div className="flex min-h-14 w-fit max-w-full flex-wrap items-center gap-1.5">
        {budgetNotice}
        {showPortrait && (
          <div className="shrink-0">
            {portraitAssetId ? (
              <div className="relative">
                <button type="button" onClick={() => setPortraitLibraryOpen(true)} className="flex h-14 w-44 items-center gap-1.5 rounded-xl border border-dashed border-cyan-300 bg-cyan-50/70 px-2 pr-7 text-left dark:border-cyan-400/30 dark:bg-cyan-400/10">
                  <UserRound size={16} className="shrink-0 text-cyan-600" />
                  <div className="min-w-0 flex-1">
                    <div className="text-[10px] font-medium text-cyan-700 dark:text-cyan-300">{portraitAssetType === "video" ? t("video.portraitVideo") : t("video.portraitImage")}</div>
                    <div className="mt-1 truncate text-xs text-gray-600 dark:text-gray-200">{portraitAssetId.replace(/^asset:\/\//, "")}</div>
                  </div>
                </button>
                <button type="button" aria-label={t("common.clear")} title={t("common.clear")} onClick={() => onPortraitAssetIdChange?.("")} className="absolute right-1.5 top-1.5 grid h-5 w-5 place-items-center rounded-full bg-white/80 text-gray-400 hover:text-red-500 dark:bg-gray-900/80"><X size={11}/></button>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => setPortraitLibraryOpen(true)}
                title={t("video.portraitAssetHint")}
                className="flex h-14 w-16 flex-col items-center justify-center gap-0.5 rounded-xl border border-dashed border-cyan-300 bg-cyan-50/60 text-cyan-700 transition hover:bg-cyan-50 dark:border-cyan-400/30 dark:bg-cyan-400/10 dark:text-cyan-300"
              >
                <Plus size={17} />
                <span className="text-[10px]">{t("video.portraitAsset")}</span>
              </button>
            )}
          </div>
        )}
        {showPortrait && <ArrowRight size={13} className="shrink-0 text-gray-300" />}
        {showImages && (
          <ReferenceImageStack references={references}
            images={media.reference_images}
            max={referenceBudget.reference_images}
            uploading={uploading}
            onUpload={(files) => uploadMany(files, referenceBudget.reference_images)}
            onRemove={removeRef}
            compact
          />
        )}
        {showImages && (showVideos || showAudios) && <ArrowRight size={13} className="shrink-0 text-gray-300" />}
        {showVideos && (
          <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-1.5">
            {media.reference_videos.map((item, index) => (
              <FilledFileCard
                key={`${item.url}-${index}`}
                item={item}
                kind="video" badge={referenceLabel(item.url, "video", media.reference_videos.slice(0, index).filter(previous => previous.url === item.url).length)}
                onRemove={() => onChange({ ...media, reference_videos: media.reference_videos.filter((_, i) => i !== index) })}
              />
            ))}
            {media.reference_videos.length < (referenceBudget.reference_videos) && (
              <EmptyUploadBox
                label={`${t("video.referenceVideo")} ${media.reference_videos.length}/${referenceBudget.reference_videos}`}
                compact
                accept={VIDEO_ACCEPT}
                uploading={uploading}
                onUpload={(files) =>
                  uploadFiles(files, "video", media.reference_videos, referenceBudget.reference_videos, (items) =>
                    onChange({ ...media, reference_videos: items })
                  )
                }
              />
            )}
          </div>
        )}
        {showVideos && showAudios && <ArrowRight size={13} className="shrink-0 text-gray-300" />}
        {showAudios && (
          <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-1.5">
            {media.reference_audios.map((item, index) => (
              <FilledFileCard
                key={`${item.url}-${index}`}
                item={item}
                kind="audio" badge={referenceLabel(item.url, "audio", media.reference_audios.slice(0, index).filter(previous => previous.url === item.url).length)}
                onRemove={() => onChange({ ...media, reference_audios: media.reference_audios.filter((_, i) => i !== index) })}
              />
            ))}
            {media.reference_audios.length < (referenceBudget.reference_audios) && (
              <EmptyUploadBox
                label={`${t("video.referenceAudio")} ${media.reference_audios.length}/${referenceBudget.reference_audios}`}
                compact
                accept={AUDIO_ACCEPT}
                uploading={uploading}
                onUpload={(files) =>
                  uploadFiles(files, "audio", media.reference_audios, referenceBudget.reference_audios, (items) =>
                    onChange({ ...media, reference_audios: items })
                  )
                }
              />
            )}
          </div>
        )}
      </div>
      <SeedancePortraitDialog
        open={portraitLibraryOpen}
        selectedId={portraitAssetId}
        onClose={() => setPortraitLibraryOpen(false)}
        onSelect={(id, type) => {
          onPortraitAssetIdChange?.(id);
          onPortraitAssetTypeChange?.(type);
          setPortraitLibraryOpen(false);
        }}
      />
      </>
    );
  }

  if (profile === "first_frame") {
    return <FrameSlot label={[t("video.firstFrame"), referenceLabel(media.first_frame?.url)].filter(Boolean).join(" · ")} image={media.first_frame} uploading={uploading} onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, first_frame: item }))} onRemove={() => onChange({ ...media, first_frame: null })} />;
  }

  if (profile === "veo_frame_pair") {
    return (
      <div className="scroll-x-only flex h-16 w-full flex-nowrap items-center gap-2">
        <FrameSlot
          label={[t("video.firstFrame"), referenceLabel(media.first_frame?.url)].filter(Boolean).join(" · ")}
          image={media.first_frame}
          uploading={uploading}
          onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, first_frame: item }))}
          onRemove={() => onChange({ ...media, first_frame: null })}
        />
        <div className="flex h-16 items-center self-center text-gray-300">
          <ArrowRight size={16} />
        </div>
        <FrameSlot
          label={[t("video.lastFrame"), referenceLabel(media.last_frame?.url)].filter(Boolean).join(" · ")}
          image={media.last_frame}
          uploading={uploading}
          onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, last_frame: item }))}
          onRemove={() => onChange({ ...media, last_frame: null })}
        />
      </div>
    );
  }

  if (profile === "frame_pair") {
    const firstLabel = t("video.firstFrame");
    const lastLabel = t("video.lastFrame");
    const max = refMax();
    return (
      <div className="scroll-x-only flex flex-nowrap items-center gap-2 w-full h-16">
        <FrameSlot
          label={firstLabel}
          image={media.first_frame}
          uploading={uploading}
          onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, first_frame: item }))}
          onRemove={() => onChange({ ...media, first_frame: null })}
        />
        <div className="flex items-center self-center text-gray-300 h-16">
          <ArrowRight size={16} />
        </div>
        <FrameSlot
          label={lastLabel}
          image={media.last_frame}
          uploading={uploading}
          onUpload={(files) => uploadOne(files, (item) => onChange({ ...media, last_frame: item }))}
          onRemove={() => onChange({ ...media, last_frame: null })}
        />
        {max > 0 && (
          <ReferenceImageStack references={references}
            images={media.reference_images}
            max={max}
            uploading={uploading}
            onUpload={(files) => uploadMany(files, max)}
            onRemove={removeRef}
          />
        )}
      </div>
    );
  }

  if (profile === "multi_ref") {
    const max = config.max_reference_images ?? 9;
    return (
      <ReferenceImageStack references={references}
        images={media.reference_images}
        max={max}
        uploading={uploading}
        onUpload={(files) => uploadMany(files, max)}
        onRemove={removeRef}
      />
    );
  }

  const max = config.max_reference_images ?? 1;
  if (max <= 0) return null;

  return (
    <ReferenceImageStack references={references}
      images={media.reference_images}
      max={max}
      uploading={uploading}
      onUpload={(files) => {
        if (max === 1 && media.reference_images.length === 0) {
          uploadOne(files, (item) => onChange({ ...media, reference_images: [item] }));
        } else {
          uploadMany(files, max);
        }
      }}
      onRemove={removeRef}
    />
  );
}
