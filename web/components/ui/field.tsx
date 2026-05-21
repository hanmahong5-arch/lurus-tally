"use client"

import type { ReactNode } from "react"
import type { FieldError } from "react-hook-form"

import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"

interface FieldProps {
  label?: ReactNode
  htmlFor?: string
  /** Pass `formState.errors.<name>` (or a plain string). */
  error?: FieldError | string
  required?: boolean
  /** Helper text shown when there is no error. */
  hint?: ReactNode
  className?: string
  children: ReactNode
}

/**
 * Field is the single label + control + error-message wrapper. Errors render as
 * `text-xs text-destructive` directly under the control, giving every form the
 * same field-level validation feedback.
 */
export function Field({
  label,
  htmlFor,
  error,
  required,
  hint,
  className,
  children,
}: FieldProps) {
  const message = typeof error === "string" ? error : error?.message

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      {label && (
        <Label htmlFor={htmlFor}>
          {label}
          {required && <span className="text-destructive"> *</span>}
        </Label>
      )}
      {children}
      {hint && !message && <p className="text-xs text-muted-foreground">{hint}</p>}
      {message && <p className="text-xs text-destructive">{message}</p>}
    </div>
  )
}
