import {HStack, Text} from '@astryxdesign/core';
import type {ReactNode} from 'react';

export function BrandMark({size = 38}: {size?: number}): ReactNode {
  return (
    <span className="fs-brand-mark" style={{width: size, height: size}} aria-hidden="true">
      <svg viewBox="0 0 64 64" width={size * 0.72} height={size * 0.72} fill="none">
        <defs>
          <linearGradient id="fs-brand-gradient" x1="8" y1="4" x2="56" y2="60" gradientUnits="userSpaceOnUse">
            <stop stopColor="#7c9cff" />
            <stop offset="1" stopColor="#8b5cf6" />
          </linearGradient>
        </defs>
        <rect x="9" y="18" width="46" height="33" rx="8" stroke="url(#fs-brand-gradient)" strokeWidth="3" />
        <path d="M9 29h46" stroke="url(#fs-brand-gradient)" strokeWidth="3" />
        <path d="M24 18v-3.2A2.8 2.8 0 0 1 26.8 12h10.4a2.8 2.8 0 0 1 2.8 2.8V18" stroke="url(#fs-brand-gradient)" strokeWidth="3" strokeLinecap="round" />
        <circle cx="45" cy="39" r="2.6" fill="#7c9cff" />
      </svg>
    </span>
  );
}

export function Brand({compact = false}: {compact?: boolean}): ReactNode {
  return (
    <HStack gap={2} align="center" className="fs-brand">
      <BrandMark size={compact ? 34 : 38} />
      {!compact && (
        <Text type="body" weight="bold" className="fs-brand-name">
          Fileshare
        </Text>
      )}
    </HStack>
  );
}
