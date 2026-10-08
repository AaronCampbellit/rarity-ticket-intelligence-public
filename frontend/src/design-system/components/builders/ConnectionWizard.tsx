import { CheckCircle2, Circle, Server, ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";

export function ConnectionWizard({
  currentStep,
  children,
}: {
  currentStep: 1 | 2 | 3;
  children: ReactNode;
}) {
  const steps = [
    { label: "Connection", icon: Server },
    { label: "Credentials", icon: ShieldCheck },
    { label: "Verify", icon: CheckCircle2 },
  ];
  return (
    <div className="rti-connection-wizard">
      <ol aria-label="Connection setup progress">
        {steps.map((step, index) => {
          const number = index + 1;
          const Icon = number <= currentStep ? step.icon : Circle;
          return (
            <li
              key={step.label}
              aria-current={number === currentStep ? "step" : undefined}
              data-complete={number < currentStep}
            >
              <Icon size={16} aria-hidden="true" />
              <span>{step.label}</span>
            </li>
          );
        })}
      </ol>
      <div>{children}</div>
    </div>
  );
}
