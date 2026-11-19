import {useState, type FormEvent, type ReactNode} from 'react';
import {Banner} from '@astryxdesign/core/Banner';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {Center} from '@astryxdesign/core/Center';
import {Heading} from '@astryxdesign/core/Heading';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {login} from '../api';
import {BrandMark} from './Brand';

export function LoginPage(): ReactNode {
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const insecure = window.location.protocol === 'http:';

  async function doSignIn(): Promise<void> {
    if (!password) {
      setError('Enter your password.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await login(password);
      window.location.replace('/');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not sign in.');
      setBusy(false);
    }
  }

  function submit(event: FormEvent): void {
    event.preventDefault();
    void doSignIn();
  }

  return (
    <div className="fs-login-page">
      <Center minHeight="100dvh" padding={4}>
        <VStack gap={3} width="100%" maxWidth={430}>
          {insecure && (
            <Banner
              status="warning"
              title="Unencrypted connection"
              description="This page is using HTTP. Sign in through the Tailscale HTTPS URL so your password is encrypted."
            />
          )}
          <Card padding={6} elevation="high" className="fs-login-card">
            <VStack gap={5}>
              <VStack gap={2} align="center" hAlign="center">
                <BrandMark size={64} />
                <Heading level={1}>Fileshare</Heading>
                <Text type="supporting" justify="center">
                  Sign in to your private drive
                </Text>
              </VStack>
              <form onSubmit={submit} noValidate>
                <VStack gap={3}>
                  <HStack gap={2} align="end">
                    <div className="fs-password-field">
                      <TextInput
                        label="Password"
                        type={showPassword ? 'text' : 'password'}
                        value={password}
                        onChange={setPassword}
                        isRequired
                        hasAutoFocus
                        isLabelHidden
                        placeholder="Password"
                        onEnter={() => {
                          if (!busy) void doSignIn();
                        }}
                      />
                    </div>
                    <Button
                      label={showPassword ? 'Hide' : 'Show'}
                      variant="ghost"
                      size="md"
                      onClick={() => setShowPassword((value) => !value)}
                    />
                  </HStack>
                  {error && <Banner status="error" title="Could not sign in" description={error} />}
                  <Button
                    label="Sign in"
                    variant="primary"
                    type="submit"
                    width="100%"
                    isLoading={busy}
                  />
                </VStack>
              </form>
            </VStack>
          </Card>
          <Text type="supporting" justify="center">
            Private server · LAN and Tailscale access only
          </Text>
        </VStack>
      </Center>
    </div>
  );
}
