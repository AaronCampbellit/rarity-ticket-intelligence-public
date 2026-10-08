import { useEffect, useRef } from "react";

type ValidationError = {
  fieldID: string;
  message: string;
};

export function ValidationSummary({
  title,
  errors,
  focusOnMount = false,
}: {
  title: string;
  errors: ValidationError[];
  focusOnMount?: boolean;
}) {
  const summaryRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (focusOnMount) summaryRef.current?.focus();
  }, [focusOnMount]);

  return (
    <div
      ref={summaryRef}
      className="rti-validation-summary"
      role="alert"
      tabIndex={-1}
    >
      <strong>{title}</strong>
      <ul>
        {errors.map((error) => (
          <li key={`${error.fieldID}-${error.message}`}>
            <a href={`#${error.fieldID}`}>{error.message}</a>
          </li>
        ))}
      </ul>
    </div>
  );
}
