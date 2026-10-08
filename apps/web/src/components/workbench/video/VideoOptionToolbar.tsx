"use client";

import { useState, type ReactNode } from "react";
import { Clock, Grid3X3, Monitor, Music2, Settings2, Sparkles, Target, Wand2 } from "lucide-react";
import {
  DEFAULT_VIDEO_COUNT_OPTIONS,
  enumLabel,
  isTopPlacementField,
  parseCountOptions,
  schemaFieldEntries,
  type SchemaFieldMeta,
  type VideoRuntimeConfig,
} from "@starai/shared-types";
import { useI18n } from "@/i18n/I18nProvider";
import { MediaMenuOption, MediaOptionMenu } from "../MediaOptionMenu";
import { SchemaForm } from "../SchemaForm";

type Translate = ReturnType<typeof useI18n>["t"];
type TranslateSource = ReturnType<typeof useI18n>["ts"];

function iconFor(name?: string): ReactNode {
  switch (name) {
    case "layers":
      return <Grid3X3 size={16} />;
    case "clock":
      return <Clock size={16} />;
    case "ratio":
      return <Monitor size={16} />;
    case "sparkles":
      return <Sparkles size={16} />;
    case "target":
      return <Target size={16} />;
    case "wand":
      return <Wand2 size={16} />;
    case "music":
      return <Music2 size={16} />;
    case "4k":
      return <span className="text-[10px] font-bold leading-none">4K</span>;
    default:
      return <Grid3X3 size={16} />;
  }
}

const FIELD_TITLE_KEY: Record<string, string> = {
  count: "imageToolbar.count",
  duration: "video.duration",
  orientation: "video.orientation",
  generation_mode: "video.generationMode",
  generate_audio: "video.generateAudio",
  ratio: "video.ratio",
  resolution: "video.resolution",
  size: "video.size",
  watermark: "video.watermark",
  aigc_watermark: "video.watermark",
  return_last_frame: "video.returnLastFrame",
  priority: "video.priority",
};

const FIELD_DESC_KEY: Record<string, string> = {
  count: "imageToolbar.countDesc",
  duration: "video.durationDesc",
  orientation: "video.orientationDesc",
  generation_mode: "video.generationModeDesc",
  generate_audio: "video.generateAudioDesc",
  ratio: "video.ratioDesc",
  resolution: "video.resolutionDesc",
  size: "video.sizeDesc",
  watermark: "video.watermarkDesc",
  aigc_watermark: "video.watermarkDesc",
  return_last_frame: "video.returnLastFrameDesc",
  priority: "video.priorityDesc",
};

function fieldTitle(t: Translate, ts: TranslateSource, key: string, prop: SchemaFieldMeta) {
  const i18nKey = FIELD_TITLE_KEY[key];
  return i18nKey ? t(i18nKey) : ts(typeof prop.title === "string" ? prop.title : key);
}

function fieldDesc(t: Translate, ts: TranslateSource, key: string, prop: SchemaFieldMeta) {
  const i18nKey = FIELD_DESC_KEY[key];
  const desc = (prop as SchemaFieldMeta & { description?: string }).description;
  if (desc) return ts(desc);
  return i18nKey ? t(i18nKey) : ts(typeof (desc || prop.title) === "string" ? String(desc || prop.title) : key);
}

function optionLabel(t: Translate, ts: TranslateSource, key: string, prop: SchemaFieldMeta, value: unknown) {
  const raw = String(value ?? "");
  if (prop.enumLabels?.[raw]) return ts(prop.enumLabels[raw]);
  const lookup = `video.option.${key}.${raw}`;
  const translated = t(lookup);
  if (translated !== lookup) return translated;
  return ts(enumLabel(prop, value) || raw);
}

function CountOptionMenu({
  prop,
  value,
  videoConfig,
  countUnit,
  onChange,
}: {
  prop: SchemaFieldMeta;
  value: unknown;
  videoConfig?: VideoRuntimeConfig;
  countUnit?: string;
  onChange: (val: number) => void;
}) {
  const { t } = useI18n();
  const unit = countUnit || t("unit.video");
  const options =
    videoConfig?.count_options?.length
      ? videoConfig.count_options
      : prop.enum?.length
        ? parseCountOptions(prop.enum)
        : DEFAULT_VIDEO_COUNT_OPTIONS;
  const allowCustom = videoConfig?.count_allow_custom !== false;
  const maxCustom = videoConfig?.count_max ?? Number(prop.maximum ?? 50) ?? 50;
  const count = Math.max(1, Number(value ?? prop.default ?? options[0] ?? 1) || 1);
  const [customDraft, setCustomDraft] = useState(String(count));

  return (
    <MediaOptionMenu
      icon={iconFor(prop["x-icon"])}
      activeLabel={`${count} ${unit}`}
      title={t("imageToolbar.count")}
      subtitle={t("imageToolbar.countDesc")}
      tone={prop["x-highlight"] ? "yellow" : "white"}
      compactOnMobile
    >
      {(closeMenu) => (
        <div className="space-y-2">
          {options.map((n) => (
            <MediaMenuOption
              key={n}
              selected={count === n}
              onClick={() => {
                onChange(n);
                setCustomDraft(String(n));
                closeMenu();
              }}
            >
              {n} {unit}
            </MediaMenuOption>
          ))}
          {allowCustom && (
            <div className="mt-3 border-t border-gray-100 pt-3 dark:border-white/10">
              <div className="mb-2 text-xs text-gray-500 dark:text-gray-400">{t("imageToolbar.customCount")}</div>
              <div className="flex items-center gap-3">
                <input
                  value={customDraft}
                  type="number"
                  min={1}
                  max={maxCustom}
                  onChange={(e) => setCustomDraft(e.target.value)}
                  className="h-10 flex-1 rounded-xl border border-gray-200 bg-white px-3 text-sm text-gray-900 focus:border-primary focus:outline-none dark:border-white/10 dark:bg-white/5 dark:text-gray-100 dark:[color-scheme:dark]"
                />
                <button
                  type="button"
                  className="h-10 rounded-xl border border-gray-900 bg-white px-4 text-sm font-semibold text-gray-900 dark:border-white/10 dark:bg-white/5 dark:text-gray-100"
                  onClick={() => {
                    const n = Math.min(maxCustom, Math.max(1, parseInt(customDraft, 10) || 1));
                    onChange(n);
                    setCustomDraft(String(n));
                    closeMenu();
                  }}
                >
                  {t("common.confirm")}
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </MediaOptionMenu>
  );
}

function renderFieldControl(
  key: string,
  prop: SchemaFieldMeta,
  value: unknown,
  onChange: (key: string, val: unknown) => void,
  t: Translate,
  ts: TranslateSource,
  videoConfig?: VideoRuntimeConfig,
  countUnit?: string
) {
  const widget = prop["x-widget"] || (prop.enum?.length ? "option_menu" : "select");

  if (key === "count" && widget === "option_menu") {
    return <CountOptionMenu prop={prop} value={value} videoConfig={videoConfig} countUnit={countUnit} onChange={(n) => onChange(key, n)} />;
  }

  if (widget === "boolean_toggle") {
    const on = Boolean(value);
    return (
      <button
        type="button"
        onClick={() => onChange(key, !on)}
        aria-label={`${fieldTitle(t, ts, key, prop)}: ${on ? "ON" : "OFF"}`}
        title={`${fieldTitle(t, ts, key, prop)}: ${on ? "ON" : "OFF"}`}
        className={`flex h-8 shrink-0 items-center gap-1.5 rounded-xl border px-2.5 text-xs shadow-sm transition ${
          on
            ? "border-primary/30 bg-primary/10 text-gray-900 dark:border-primary/30 dark:bg-primary/15 dark:text-gray-100"
            : "border-gray-200 bg-white text-gray-700 dark:border-white/10 dark:bg-white/5 dark:text-gray-200"
        }`}
      >
        <span className="text-gray-500 dark:text-gray-400">{iconFor(prop["x-icon"])}</span>
        <span className="hidden sm:inline">
          {fieldTitle(t, ts, key, prop)}: {on ? "ON" : "OFF"}
        </span>
      </button>
    );
  }

  const options = prop.enum || [];
  if (!options.length) return <SchemaForm schema={{ properties: { [key]: prop } }} values={{ [key]: value ?? prop.default ?? "" }} onChange={(next) => onChange(key, next[key])} />;
  const activeLabel = optionLabel(t, ts, key, prop, value ?? prop.default ?? options[0]);

  return (
    <MediaOptionMenu
      icon={iconFor(prop["x-icon"])}
      activeLabel={String(activeLabel)}
      title={fieldTitle(t, ts, key, prop)}
      subtitle={fieldDesc(t, ts, key, prop)}
      tone={prop["x-highlight"] ? "yellow" : "white"}
      compactOnMobile
    >
      {(closeMenu) => (
        <div className="space-y-2">
          {options.map((opt) => {
            const selected = String(value ?? "") === String(opt);
            return (
              <MediaMenuOption
                key={String(opt)}
                selected={selected}
                onClick={() => {
                  onChange(key, opt);
                  closeMenu();
                }}
              >
                {optionLabel(t, ts, key, prop, opt)}
              </MediaMenuOption>
            );
          })}
        </div>
      )}
    </MediaOptionMenu>
  );
}

function VideoSettingsMenu({
  entries,
  values,
  onChange,
  t,
  ts,
}: {
  entries: Array<[string, SchemaFieldMeta]>;
  values: Record<string, unknown>;
  onChange: (key: string, val: unknown) => void;
  t: Translate;
  ts: TranslateSource;
}) {
  return (
    <MediaOptionMenu
      icon={<Settings2 size={16} />}
      activeLabel={ts("设置")}
      title={ts("设置")}
      subtitle={entries.map(([key, prop]) => fieldTitle(t, ts, key, prop)).join(" · ")}
      menuWidth={320}
      compactOnMobile
    >
      {() => (
        <div className="space-y-1.5">
          {entries.map(([key, prop]) => {
            const label = fieldTitle(t, ts, key, prop);
            const value = values[key] ?? prop.default ?? prop.enum?.[0];
            if (prop["x-widget"] === "boolean_toggle") {
              const enabled = Boolean(value);
              return (
                <button
                  key={key}
                  type="button"
                  role="switch"
                  aria-checked={enabled}
                  onClick={() => onChange(key, !enabled)}
                  className="flex h-10 w-full items-center justify-between rounded-xl bg-gray-50 px-3 text-sm font-medium text-gray-800 transition hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:bg-white/5 dark:text-gray-200 dark:hover:bg-white/10"
                >
                  <span>{label}</span>
                  <span className={`relative h-5 w-9 rounded-full transition ${enabled ? "bg-primary" : "bg-gray-300 dark:bg-gray-600"}`}>
                    <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${enabled ? "translate-x-[18px]" : "translate-x-0.5"}`} />
                  </span>
                </button>
              );
            }

            const options = prop.enum || [];
            if (!options.length) return <SchemaForm key={key} schema={{ properties: { [key]: prop } }} values={{ [key]: value ?? "" }} onChange={(next) => onChange(key, next[key])} />;
            return (
              <label key={key} className="flex min-h-10 items-center justify-between gap-3 rounded-xl bg-gray-50 px-3 py-1.5 dark:bg-white/5">
                <span className="shrink-0 text-sm font-medium text-gray-800 dark:text-gray-200">{label}</span>
                <select
                  value={String(value ?? "")}
                  aria-label={label}
                  onChange={(event) => {
                    const selected = options.find((option) => String(option) === event.target.value);
                    onChange(key, selected ?? event.target.value);
                  }}
                  className="min-w-0 max-w-[170px] rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-right text-xs text-gray-800 outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/15 dark:border-white/10 dark:bg-gray-900 dark:text-gray-200 dark:[color-scheme:dark]"
                >
                  {options.map((option) => (
                    <option key={String(option)} value={String(option)}>{optionLabel(t, ts, key, prop, option)}</option>
                  ))}
                </select>
              </label>
            );
          })}
        </div>
      )}
    </MediaOptionMenu>
  );
}

export function VideoOptionToolbar({
  schema,
  values,
  onChange,
  videoConfig,
  countUnit,
}: {
  schema: unknown;
  values: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  videoConfig?: VideoRuntimeConfig;
  countUnit?: string;
}) {
  const { t, ts } = useI18n();
  const set = (key: string, val: unknown) => onChange({ ...values, [key]: val });
  const entries = schemaFieldEntries(schema).filter(([, prop]) => !isTopPlacementField(prop));
  if (entries.length === 0) return null;
  const settingsEntries = entries.filter(([, prop]) => prop["x-group"] === "settings");
  const quickEntries = entries.filter(([, prop]) => prop["x-group"] !== "settings");
  return (
    <>
      {quickEntries.slice(0, 1).map(([key, prop]) => <span key={key}>{renderFieldControl(key, prop, values[key], set, t, ts, videoConfig, countUnit)}</span>)}
      {settingsEntries.length > 0 && <VideoSettingsMenu entries={settingsEntries} values={values} onChange={set} t={t} ts={ts} />}
      {quickEntries.slice(1).map(([key, prop]) => <span key={key}>{renderFieldControl(key, prop, values[key], set, t, ts, videoConfig, countUnit)}</span>)}
    </>
  );
}

export function VideoTopControls({
  schema,
  values,
  onChange,
  videoConfig,
  countUnit,
}: {
  schema: unknown;
  values: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  videoConfig?: VideoRuntimeConfig;
  countUnit?: string;
}) {
  const { t, ts } = useI18n();
  const set = (key: string, val: unknown) => onChange({ ...values, [key]: val });
  const entries = schemaFieldEntries(schema).filter(([, prop]) => isTopPlacementField(prop));
  const priorityIndex = entries.findIndex(([key]) => key === "priority");
  const audioIndex = entries.findIndex(([key]) => key === "generate_audio");
  if (priorityIndex >= 0 && audioIndex >= 0 && priorityIndex > audioIndex) {
    [entries[priorityIndex], entries[audioIndex]] = [entries[audioIndex], entries[priorityIndex]];
  }
  if (entries.length === 0) return null;
  return <>{entries.map(([key, prop]) => <span key={key}>{renderFieldControl(key, prop, values[key], set, t, ts, videoConfig, countUnit)}</span>)}</>;
}
