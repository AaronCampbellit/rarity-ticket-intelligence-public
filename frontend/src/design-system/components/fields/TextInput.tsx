import type { InputHTMLAttributes } from "react";

export function TextInput({
  className = "",
  type = "text",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input {...props} type={type} className={`rti-input ${className}`.trim()} />
  );
}
