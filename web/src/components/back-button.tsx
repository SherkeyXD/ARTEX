"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeftIcon } from "lucide-react";
import { Button, type ButtonProps } from "@/components/ui/button";

export interface BackButtonProps extends Omit<ButtonProps, "asChild"> {
  fallback: string;
  title?: string;
  children?: React.ReactNode;
}

export function BackButton({
  fallback,
  title = "返回",
  children,
  variant = "ghost",
  size = "icon",
  className,
  ...props
}: BackButtonProps) {
  const router = useRouter();

  const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
    // If user presses modifier keys (Ctrl/Cmd/Shift/Alt), let standard browser link behavior handle it (e.g. open in new tab).
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) {
      return;
    }
    e.preventDefault();

    // Check if we can safely go back in browser history within the same origin
    const canGoBack =
      typeof window !== "undefined" &&
      window.history.length > 1 &&
      (!document.referrer || new URL(document.referrer, window.location.origin).origin === window.location.origin);

    if (canGoBack) {
      router.back();
    } else {
      router.push(fallback);
    }
  };

  return (
    <Button
      asChild
      variant={variant}
      size={size}
      className={className}
      title={title}
      aria-label={title}
      {...props}
    >
      <Link href={fallback} onClick={handleClick}>
        {children ?? <ArrowLeftIcon />}
      </Link>
    </Button>
  );
}
