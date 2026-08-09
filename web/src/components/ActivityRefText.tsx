import type React from 'react';
import { EntityRefText } from './EntityRefText';

interface ActivityRefTextProps {
  /** The plain text (a ref field value or pretty-printed payload JSON) to linkify. */
  text: string;
  className?: string;
  /** "id" keeps literal payload text; "label" shows short refs/display names. */
  variant?: 'id' | 'label';
}

// ActivityRefText is now a thin compatibility wrapper over the shared EntityRef
// tokenizer/resolver/renderer. Activity debug JSON keeps literal ids by default,
// while human-facing audit sentences opt into short labels via variant="label".
export function ActivityRefText({ text, className, variant = 'id' }: ActivityRefTextProps): React.ReactElement {
  return (
    <EntityRefText
      text={text}
      className={className}
      variant={variant}
      surface="activity"
      linkClass="text-accent"
      agentMode="link"
    />
  );
}
