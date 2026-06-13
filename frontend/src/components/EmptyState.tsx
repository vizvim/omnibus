import type { ReactNode } from "react";

// EmptyState renders the contracted empty/error copy: a heading, body, and optional CTA,
// with an optional icon glyph above. Presented inside a soft dashed panel so empty screens
// read as intentional rather than broken.
export function EmptyState({
  heading,
  body,
  cta,
  icon,
}: {
  heading: string;
  body: string;
  cta?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-4 rounded-2xl border border-dashed border-border-strong/50 bg-card/30 px-6 py-16 text-center">
      {icon ? (
        <div className="flex size-14 items-center justify-center rounded-2xl border border-border bg-secondary/60 text-primary [&_svg]:size-6">
          {icon}
        </div>
      ) : null}
      <h2 className="font-display text-xl font-semibold tracking-tight text-foreground">
        {heading}
      </h2>
      <p className="max-w-md text-sm leading-relaxed text-muted-foreground">{body}</p>
      {cta ? <div className="mt-1">{cta}</div> : null}
    </div>
  );
}
