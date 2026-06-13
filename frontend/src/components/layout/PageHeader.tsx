import type { ReactNode } from "react";

// PageHeader is the consistent screen header: a mono eyebrow, a display title, an optional
// muted description, and a right-aligned actions slot.
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4 border-b border-border pb-5">
      <div className="flex flex-col gap-1.5">
        {eyebrow ? (
          <span className="font-mono text-[0.7rem] uppercase tracking-[0.2em] text-primary/70">
            {eyebrow}
          </span>
        ) : null}
        <h1 className="font-display text-3xl font-semibold tracking-tight text-foreground">
          {title}
        </h1>
        {description ? (
          <p className="text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}
