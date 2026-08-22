export function ErrorBanner({ message }: { message: string }) {
  if (!message) return null;
  return (
    <div className="text-sm text-destructive bg-destructive/10 px-4 py-3 rounded-lg">
      {message}
    </div>
  );
}
