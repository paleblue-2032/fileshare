import type {ReactNode} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {DropdownMenu, type DropdownMenuOption} from '@astryxdesign/core/DropdownMenu';
import {Icon} from '@astryxdesign/core/Icon';
import {IconButton} from '@astryxdesign/core/IconButton';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Text} from '@astryxdesign/core/Text';
import {
  File,
  FileArchive,
  FileAudio,
  FileCode2,
  FileImage,
  FileText,
  FileVideo,
  Folder,
  MoreHorizontal,
} from 'lucide-react';

const kindIcons = {
  folder: Folder,
  image: FileImage,
  video: FileVideo,
  audio: FileAudio,
  pdf: FileText,
  text: FileCode2,
  archive: FileArchive,
  file: File,
} as const;

export function kindIcon(kind: string) {
  return kindIcons[kind as keyof typeof kindIcons] ?? File;
}

type RowProps = {
  kind: string;
  name: string;
  description: ReactNode;
  size?: string;
  date?: string;
  mainLabel?: string;
  onMain?: () => void;
  menuItems: DropdownMenuOption[];
};

export function Row({
  kind,
  name,
  description,
  size,
  date,
  mainLabel,
  onMain,
  menuItems,
}: RowProps): ReactNode {
  const content = (
    <HStack gap={3} align="center" className="fs-row-content">
      <span className={`fs-icon fs-icon-${kind}`}>
        <Icon icon={kindIcon(kind)} size="md" />
      </span>
      <VStack gap={0.5} className="fs-row-text">
        <Text type="body" weight="medium" maxLines={1}>
          {name}
        </Text>
        <Text type="supporting" size="xsm" maxLines={1}>
          {description}
        </Text>
      </VStack>
    </HStack>
  );

  return (
    <div className="fs-row">
      {onMain ? (
        <Button
          label={mainLabel ?? name}
          variant="ghost"
          className="fs-row-main"
          width="100%"
          onClick={onMain}
        >
          {content}
        </Button>
      ) : (
        <div className="fs-row-main fs-row-static">{content}</div>
      )}
      <Text type="supporting" className="fs-row-size" maxLines={1}>
        {size ?? '—'}
      </Text>
      <Text type="supporting" className="fs-row-date" maxLines={1}>
        {date ?? '—'}
      </Text>
      {menuItems.length > 0 && (
        <DropdownMenu
          items={menuItems}
          renderTrigger={(props) => (
            <IconButton
              {...props}
              label="More actions"
              tooltip="More actions"
              variant="ghost"
              icon={<Icon icon={MoreHorizontal} size="sm" />}
            />
          )}
        />
      )}
    </div>
  );
}
