"use client"

import type { FormHTMLAttributes, ReactNode } from "react"
import {
  FormProvider,
  type FieldValues,
  type SubmitHandler,
  type UseFormReturn,
} from "react-hook-form"

interface FormProps<T extends FieldValues>
  extends Omit<FormHTMLAttributes<HTMLFormElement>, "onSubmit"> {
  form: UseFormReturn<T>
  onSubmit: SubmitHandler<T>
  children: ReactNode
}

/**
 * Form binds a react-hook-form instance to a native <form>. Pair with
 * `useForm({ resolver: zodResolver(schema) })` and the Field component so every
 * form shares one validation + error-display path. `noValidate` hands all
 * validation to zod (no browser bubble).
 */
export function Form<T extends FieldValues>({
  form,
  onSubmit,
  children,
  ...props
}: FormProps<T>) {
  return (
    <FormProvider {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate {...props}>
        {children}
      </form>
    </FormProvider>
  )
}
