import type { ReactNode } from "react";

// EmptyState renders the contracted empty/error copy: a heading, body, and optional CTA.
export function EmptyState({
  heading,
  body,
  cta,
}: {
  heading: string;
  body: string;
  cta?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-4 py-12 text-center">
      <h2 className="text-xl font-semibold">{heading}</h2>
      <p className="max-w-md text-base text-slate-600">{body}</p>
      {cta}
    </div>
  );
}
