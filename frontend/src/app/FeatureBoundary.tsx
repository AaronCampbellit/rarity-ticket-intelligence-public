import { Component, type ReactNode } from "react";

import { Button, StatePanel, useMainContentID } from "../design-system";

export class FeatureBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return <FeatureError />;
    }
    return this.props.children;
  }
}
function FeatureError() {
  const mainContentID = useMainContentID();
  return (
    <main id={mainContentID} tabIndex={-1}>
      <StatePanel
        state="error"
        title="Workspace could not be loaded"
        description="Reload to retrieve the current application version."
        action={
          <Button onClick={() => window.location.reload()}>
            Reload application
          </Button>
        }
      />
    </main>
  );
}
