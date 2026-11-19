import type {ReactNode} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Text} from '@astryxdesign/core/Text';
import {ProgressBar} from '@astryxdesign/core/ProgressBar';
import type {UploadItem} from '../types';
import {formatBytes} from '../utils';

type Props = {
  uploads: UploadItem[];
  onClear: () => void;
};

export function UploadTray({uploads, onClear}: Props): ReactNode {
  if (uploads.length === 0) return null;
  return (
    <div className="fs-upload-tray">
      <Card padding={3} elevation="high">
        <VStack gap={2}>
          <HStack justify="between" align="center">
            <Text type="body" weight="semibold">
              Uploads
            </Text>
            <Button label="Clear" variant="ghost" size="sm" onClick={onClear} />
          </HStack>
          <VStack gap={2}>
            {uploads.map((item) => (
              <div key={item.id} className="fs-upload-item">
                <HStack justify="between" gap={2} align="start">
                  <Text type="body" size="sm" maxLines={1} className="fs-upload-name">
                    {item.name}
                  </Text>
                  <Text type="supporting" size="xsm">
                    {item.status === 'done'
                      ? formatBytes(item.size)
                      : item.status === 'error'
                        ? item.error ?? 'Failed'
                        : item.status === 'uploading'
                          ? `${Math.round(item.progress)}%`
                          : 'Queued'}
                  </Text>
                </HStack>
                <ProgressBar
                  label={item.name}
                  isLabelHidden
                  value={item.progress}
                  max={100}
                  variant={
                    item.status === 'error'
                      ? 'error'
                      : item.status === 'done'
                        ? 'success'
                        : 'accent'
                  }
                />
              </div>
            ))}
          </VStack>
        </VStack>
      </Card>
    </div>
  );
}
