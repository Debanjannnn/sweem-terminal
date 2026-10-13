"use client";

import { CircleCheckIcon, Loader2Icon, OctagonXIcon, TriangleAlertIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { errMsg, shortHash } from "@/lib/api";
import { txUrl } from "@/lib/config";
import { Button } from "@/components/ui";
import { cn } from "@/lib/utils";

type Tone = "success" | "warning" | "error" | "loading";

const TONE_ICON = {
  success: <CircleCheckIcon className="size-4 text-positive" />,
  warning: <TriangleAlertIcon className="size-4 text-warning" />,
  error: <OctagonXIcon className="size-4 text-destructive" />,
  loading: <Loader2Icon className="size-4 animate-spin text-muted-foreground" />,
} satisfies Record<Tone, React.ReactNode>;

type Card = { tone: Tone; title: string; description?: string; hash?: string };

function ToastCard({ tone, title, description, hash, onDismiss }: Card & { onDismiss: () => void }) {
  return (
    <div className="w-[356px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-2xl border border-border bg-popover p-4 text-popover-foreground shadow-lg">
      <div className="flex items-start gap-2.5">
        <span className="mt-0.5 shrink-0">{TONE_ICON[tone]}</span>
        <p
          className={cn(
            "min-w-0 flex-1 text-sm leading-5 font-medium break-words",
            tone === "warning" && "text-warning",
            tone === "error" && "text-destructive",
          )}
        >
          {title}
        </p>
        <button
          type="button"
          aria-label="Dismiss"
          onClick={onDismiss}
          className="-mr-1 -mt-1 shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <XIcon className="size-3.5" />
        </button>
      </div>
      {(description || hash) && (
        <p className="mt-1.5 text-xs leading-4 break-words text-muted-foreground">
          {[description, hash && `tx ${shortHash(hash)}`].filter(Boolean).join(" · ")}
        </p>
      )}
      {hash && (
        <div className="mt-3 flex justify-end">
          <Button
            variant="ghost"
            className="px-3 py-1 text-xs"
            onClick={() => window.open(txUrl(hash), "_blank", "noopener,noreferrer")}
          >
            Explorer
          </Button>
        </div>
      )}
    </div>
  );
}

// Same id replaces the card in place: loading → success/error.
export function showToast(card: Card, id?: string | number): string | number {
  const duration = card.tone === "loading" || card.tone === "error" ? Infinity : 6000;
  return toast.custom((t) => <ToastCard {...card} onDismiss={() => toast.dismiss(t)} />, { id, duration });
}

export const toastError = (e: unknown, id?: string | number) => showToast({ tone: "error", title: errMsg(e) }, id);
