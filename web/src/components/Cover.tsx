import { useState } from "react";
import clsx from "clsx";
import { BookImage } from "lucide-react";

export function Cover({ src, alt, className }: { src?: string; alt: string; className?: string }) {
  const [failed, setFailed] = useState(false);
  return (
    <div className={clsx("relative overflow-hidden rounded-md bg-panel-2", className)}>
      {src && !failed ? (
        <img src={src} alt={alt} loading="lazy" onError={() => setFailed(true)} className="h-full w-full object-cover" />
      ) : (
        <div className="flex h-full w-full items-center justify-center text-muted">
          <BookImage className="size-8" />
        </div>
      )}
    </div>
  );
}
