// Small presentational building blocks: icons, cards, status pills, buttons.
import { useEffect, useRef, useState, type ReactNode } from "react";

type IconName =
  | "sparkles"
  | "arrow"
  | "play"
  | "wave"
  | "copy"
  | "check"
  | "info"
  | "alert"
  | "book"
  | "chevron";

const paths: Record<IconName, ReactNode> = {
  sparkles: (
    <>
      <path d="M12 3l1.8 4.7L18.5 9.5l-4.7 1.8L12 16l-1.8-4.7L5.5 9.5l4.7-1.8z" />
      <path d="M19 15l.8 2.2L22 18l-2.2.8L19 21l-.8-2.2L16 18l2.2-.8z" />
    </>
  ),
  arrow: <path d="M5 12h14M13 6l6 6-6 6" />,
  play: <path d="M8 5.5v13l10.5-6.5z" />,
  wave: <path d="M3 12h2M7 8v8M11 5v14M15 9v6M19 7v10M21 12h0" />,
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </>
  ),
  check: <path d="M5 12.5l4.5 4.5L19 7.5" />,
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 7.5v.5" />
    </>
  ),
  alert: (
    <>
      <path d="M12 3.5L22 20H2z" />
      <path d="M12 10v4.5M12 17.2v.3" />
    </>
  ),
  book: (
    <>
      <path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H20v15H6.5A2.5 2.5 0 0 0 4 20.5z" />
      <path d="M4 20.5A2.5 2.5 0 0 0 6.5 23H20v-5" />
    </>
  ),
  chevron: <path d="M8 10l4 4 4-4" />,
};

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}

export function Card({
  step,
  title,
  subtitle,
  actions,
  className = "",
  children,
}: {
  step?: string;
  title: string;
  subtitle?: ReactNode;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={`card ${className}`}>
      <header className="card-head">
        {step && (
          <span className="step" aria-hidden="true">
            {step}
          </span>
        )}
        <div className="card-titles">
          <h2>{title}</h2>
          {subtitle && <p className="card-sub">{subtitle}</p>}
        </div>
        {actions && <div className="card-actions">{actions}</div>}
      </header>
      {children}
    </section>
  );
}

export type Status = "ok" | "off" | "pending";

export function StatusPill({ label, status, hint }: { label: string; status: Status; hint: string }) {
  return (
    <span className={`pill pill-${status}`} title={hint}>
      <span className="dot" aria-hidden="true" />
      {label}
      <span className="visually-hidden">: {hint}</span>
    </span>
  );
}

export function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  if (!text) return null;
  return (
    <button
      type="button"
      className="icon-button"
      aria-label={copied ? "Copied" : `Copy ${label}`}
      title={copied ? "Copied" : `Copy ${label}`}
      onClick={() => {
        void navigator.clipboard?.writeText(text).then(() => {
          setCopied(true);
          window.clearTimeout(timer.current);
          timer.current = window.setTimeout(() => setCopied(false), 1500);
        });
      }}
    >
      <Icon name={copied ? "check" : "copy"} size={16} />
    </button>
  );
}

export function Spinner() {
  return <span className="spinner" aria-hidden="true" />;
}

export function Notice({ kind, children }: { kind: "error" | "warn" | "info"; children: ReactNode }) {
  return (
    <div className={`notice notice-${kind}`} role={kind === "error" ? "alert" : undefined}>
      <Icon name={kind === "info" ? "info" : "alert"} size={18} />
      <div>{children}</div>
    </div>
  );
}

/** Seconds since `running` became true, for long model calls. */
export function useElapsed(running: boolean): number {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    if (!running) return;
    setSeconds(0);
    const start = Date.now();
    const id = window.setInterval(() => setSeconds(Math.floor((Date.now() - start) / 1000)), 500);
    return () => window.clearInterval(id);
  }, [running]);
  return seconds;
}
