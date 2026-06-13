import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";

import { cn } from "../../lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 font-mono text-[0.7rem] font-medium uppercase tracking-wider transition-colors",
  {
    variants: {
      variant: {
        default: "border-primary/30 bg-primary/12 text-primary",
        neutral: "border-border-strong/50 bg-secondary text-muted-foreground",
        success: "border-tn-green/30 bg-tn-green/12 text-tn-green",
        warning: "border-tn-yellow/30 bg-tn-yellow/12 text-tn-yellow",
        info: "border-tn-cyan/30 bg-tn-cyan/12 text-tn-cyan",
        magenta: "border-tn-magenta/30 bg-tn-magenta/12 text-tn-magenta",
        danger: "border-tn-red/30 bg-tn-red/12 text-tn-red",
        outline: "border-border-strong text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {
  dot?: boolean;
}

function Badge({ className, variant, dot = false, children, ...props }: BadgeProps) {
  return (
    <span className={cn(badgeVariants({ variant }), className)} {...props}>
      {dot ? <span className="size-1.5 rounded-full bg-current" aria-hidden="true" /> : null}
      {children}
    </span>
  );
}

export { Badge, badgeVariants };
