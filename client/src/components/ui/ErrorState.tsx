export const ErrorState = ({ message }: { message: string }) => (
  <p className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-xs break-words text-destructive">
    {message}
  </p>
);
