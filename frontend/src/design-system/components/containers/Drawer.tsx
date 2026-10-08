import { Dialog, type DialogProps } from "./Dialog";

export function Drawer(props: Omit<DialogProps, "variant">) {
  return <Dialog {...props} variant="drawer" />;
}
