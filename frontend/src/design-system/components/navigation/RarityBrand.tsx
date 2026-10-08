export type RarityBrandProps = {
  variant: "full" | "compact";
  href?: string;
  onNavigate?: () => void;
};

export function RarityBrand({ variant, href, onNavigate }: RarityBrandProps) {
  const className = `rti-product-brand rti-product-brand--${variant}`;
  const content =
    variant === "full" ? (
      <img src="/brand/rarity-logo.png" width="525" height="145" alt="Rarity" />
    ) : (
      <>
        <img
          src="/brand/rarity-symbol.png"
          width="83"
          height="80"
          alt="Rarity symbol"
        />
        <strong>Rarity</strong>
      </>
    );

  return href ? (
    <a
      className={className}
      href={href}
      aria-label="Rarity home"
      onClick={onNavigate}
    >
      {content}
    </a>
  ) : (
    <div className={className}>{content}</div>
  );
}
