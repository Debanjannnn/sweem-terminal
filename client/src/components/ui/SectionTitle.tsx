// every card heading in the terminal, so they cannot drift apart again
export const SectionTitle = ({
  children,
  as: Tag = "h2",
}: {
  children: React.ReactNode;
  as?: "h1" | "h2";
}) => <Tag className="text-xs uppercase tracking-wide text-muted-foreground">{children}</Tag>;
