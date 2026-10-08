export type ToastMessage = {
  id: string;
  message: string;
};

export function ToastRegion({ messages }: { messages: ToastMessage[] }) {
  return (
    <div className="rti-toast-region" role="status" aria-live="polite">
      {messages.map((message) => (
        <div className="rti-toast" key={message.id}>
          {message.message}
        </div>
      ))}
    </div>
  );
}
