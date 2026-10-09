"use client";

import { useEffect, useId, useRef, useState, type TextareaHTMLAttributes } from "react";
import { createPortal } from "react-dom";
import { AtSign, Film, ImageIcon, Music2 } from "lucide-react";
import { invalidVideoReferences, reconcileVideoReferences, videoMentionQuery, videoReferenceToken, type VideoPromptReference } from "@starai/shared-types";
import { useI18n } from "@/i18n/I18nProvider";

type Props = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  enabled: boolean;
  references: VideoPromptReference[];
  value: string;
  onValueChange: (value: string) => void;
};

export function VideoPromptTextarea({ enabled, references, onValueChange, ...props }: Props) {
  return enabled ? <ReferenceTextarea {...props} references={references} onValueChange={onValueChange} /> : <textarea {...props} />;
}

function ReferenceTextarea({ references, onValueChange, ...props }: Omit<Props, "enabled">) {
  const { t, locale } = useI18n();
  const id = useId();
  const input = useRef<HTMLTextAreaElement>(null);
  const previous = useRef(references);
  const lastSelection = useRef({ value: "", start: -1, end: -1 });
  const [query, setQuery] = useState<ReturnType<typeof videoMentionQuery>>(null);
  const [tab, setTab] = useState("all");
  const [selected, setSelected] = useState(0);
  const [position, setPosition] = useState({ left: 0, top: 0, width: 280, above: false });
  const choices = references.filter(item => (tab === "all" || item.kind === tab) && (!query?.query || `${videoReferenceToken(item, locale)} ${item.name} ${item.kind}`.toLowerCase().includes(query.query.toLowerCase())));
  const invalid = invalidVideoReferences(props.value, references);

  useEffect(() => {
    const reconciled = reconcileVideoReferences(props.value, previous.current, references);
    previous.current = references;
    if (reconciled !== props.value) onValueChange(reconciled);
  }, [references, props.value, onValueChange]);

  useEffect(() => {
    if (query) document.getElementById(`${id}-${selected}`)?.scrollIntoView({ block: "nearest" });
  }, [id, selected, query, tab]);

  const updateQuery = () => {
    const element = input.current;
    if (!element) return;
    const next = element.selectionStart === element.selectionEnd ? videoMentionQuery(element.value, element.selectionStart) : null;
    lastSelection.current = { value: element.value, start: element.selectionStart, end: element.selectionEnd };
    if (!query && next) setTab("all");
    if (next?.start !== query?.start || next?.end !== query?.end || next?.query !== query?.query) setSelected(0);
    setQuery(next);
    const rect = element.getBoundingClientRect();
    const width = Math.min(320, window.innerWidth - 24);
    const above = rect.top >= 280;
    setPosition({ left: Math.max(12, Math.min(rect.left + 12, window.innerWidth - width - 12)), top: above ? rect.top : rect.bottom, width, above });
  };
  const insert = (reference: VideoPromptReference) => {
    const element = input.current;
    if (!element) return;
    const start = query?.start ?? element.selectionStart;
    const end = query?.end ?? element.selectionEnd;
    const token = `${videoReferenceToken(reference, locale)} `;
    onValueChange(`${props.value.slice(0, start)}${token}${props.value.slice(end)}`);
    setQuery(null);
    requestAnimationFrame(() => { element.focus(); element.setSelectionRange(start + token.length, start + token.length); });
  };

  return <div className="relative flex min-w-0 flex-1 flex-col">
    <textarea {...props} ref={input} aria-invalid={invalid.length > 0 || undefined} aria-describedby={invalid.length ? `${id}-error` : props["aria-describedby"]}
      aria-autocomplete="list" aria-controls={query ? id : undefined} aria-activedescendant={query && choices[selected] ? `${id}-${selected}` : undefined}
      onChange={event => { props.onChange?.(event); updateQuery(); }}
      onSelect={event => {
        props.onSelect?.(event);
        const element = event.currentTarget;
        const previous = lastSelection.current;
        if (element.value !== previous.value || element.selectionStart !== previous.start || element.selectionEnd !== previous.end) updateQuery();
      }}
      onBlur={event => { setQuery(null); props.onBlur?.(event); }}
      onKeyDown={event => {
        if (!event.nativeEvent.isComposing && event.keyCode !== 229 && query) {
          if (event.key === "Escape") { event.preventDefault(); setQuery(null); return; }
          if (["ArrowDown", "ArrowUp"].includes(event.key) && choices.length) {
            event.preventDefault(); setSelected(index => (index + (event.key === "ArrowDown" ? 1 : -1) + choices.length) % choices.length); return;
          }
          if ((event.key === "Enter" || event.key === "Tab") && !event.shiftKey) {
            event.preventDefault(); if (choices[selected]) insert(choices[selected]); return;
          }
        }
        props.onKeyDown?.(event);
      }} />
    <div className="flex items-center gap-2 px-4 pb-2 text-[11px] text-gray-400 dark:text-gray-500">
      <button type="button" aria-label={t("video.mentionOpen")} title={t("video.mentionOpen")} className="rounded-md p-1 text-primary hover:bg-primary/10 disabled:opacity-40" disabled={!references.length}
        onMouseDown={event => event.preventDefault()} onClick={() => {
          const element = input.current;
          if (!element) return;
          element.focus();
          if (videoMentionQuery(element.value, element.selectionStart)) { updateQuery(); return; }
          const start = element.selectionStart;
          onValueChange(`${props.value.slice(0, start)}@${props.value.slice(element.selectionEnd)}`);
          requestAnimationFrame(() => { element.setSelectionRange(start + 1, start + 1); updateQuery(); });
        }}><AtSign size={15} /></button>
      <span>{t("video.mentionHint")}</span>
    </div>
    {invalid.length > 0 && <p id={`${id}-error`} role="alert" className="px-4 pb-2 text-xs text-red-500">{t("video.mentionInvalid", { references: [...new Set(invalid)].join("、") })}</p>}
    {query && createPortal(<div className="fixed z-[200] overflow-hidden rounded-xl border border-gray-200 bg-white p-1.5 text-gray-800 shadow-xl dark:border-white/10 dark:bg-[#171e2c] dark:text-gray-100"
      style={{ left: position.left, top: position.top, width: position.width, transform: position.above ? "translateY(-100%)" : undefined }}
      onMouseDown={event => event.preventDefault()}>
      <div className="mb-1 flex gap-1 border-b border-gray-100 pb-1 dark:border-white/10">
        {["all", ...new Set(references.map(item => item.kind))].map(kind => <button type="button" key={kind} onClick={() => { setTab(kind); setSelected(0); }} className={`rounded-md px-2 py-1 text-xs ${tab === kind ? "bg-primary/10 text-primary" : "text-gray-500 dark:text-gray-400"}`}>{t(kind === "all" ? "common.all" : `canvas.kind.${kind}`)}</button>)}
      </div>
      <div id={id} role="listbox" aria-label={t("video.mentionOpen")} className="max-h-52 overflow-y-auto">
        {choices.length ? choices.map((item, index) => <button id={`${id}-${index}`} type="button" role="option" aria-selected={selected === index} key={`${item.kind}-${item.index}-${item.url}`}
          onMouseEnter={() => setSelected(index)} onClick={() => insert(item)} className={`flex w-full items-center gap-2 rounded-lg p-2 text-left text-sm ${selected === index ? "bg-primary/10" : "hover:bg-gray-50 dark:hover:bg-white/5"}`}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          {item.kind === "image" ? item.url.startsWith("asset://") ? <ImageIcon size={24} className="m-1.5 text-primary" /> : <img src={item.url} alt="" loading="lazy" className="h-9 w-9 rounded-md object-cover" /> : item.kind === "audio" ? <Music2 size={24} className="m-1.5 text-primary" /> : <Film size={24} className="m-1.5 text-primary" />}
          <span className="min-w-0"><span className="block">{videoReferenceToken(item, locale)}</span><span className="block truncate text-[10px] text-gray-400">{item.name}</span></span>
        </button>) : <p className="p-3 text-xs text-gray-400">{t("video.mentionEmpty")}</p>}
      </div>
    </div>, document.body)}
  </div>;
}
