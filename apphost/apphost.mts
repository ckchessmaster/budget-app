import { createBuilder } from './.aspire/modules/aspire.mjs';

const builder = await createBuilder();

// Temporal and the vault master key are required dependencies, not optional ones: the API
// exits at boot without them. This key is a fixed development value so the API and the
// worker agree across restarts; production supplies a real one through the Helm chart and
// the Secret, never from this file.
const temporalNamespace = 'budget-app';
const devVaultMasterKey = await builder.addParameter('vault-master-key', {
  value: 'YnVkZ2V0LWFwcC1kZXZlbG9wbWVudC12YXVsdC1rZXk=',
  secret: true
});

// TASK-1.1.2: PostgreSQL 16 Resource
const dbPassword = await builder.addParameter('db-password', {
  value: 'postgrespassword',
  secret: true
});

export const postgres = await builder
  .addPostgres('postgres', {
    port: 5432,
    password: dbPassword
  })
  .withDataVolume({ name: 'budget-local-db' });

export const budgetDb = await postgres.addDatabase('budgetdb', {
  databaseName: 'budgetdb'
});

// TASK-1.1.3: Dex OIDC Dev Container
export const dex = await builder
  .addContainer('dex', 'ghcr.io/dexidp/dex:latest')
  .withContainerFiles('/etc/dex', '../dex')
  .withHttpEndpoint({ port: 5556, targetPort: 5556, name: 'dex' })
  .withArgs(['dex', 'serve', '/etc/dex/config.yaml']);

// TASK-1.1.4: Temporal Dev Server Container
export const temporal = await builder
  .addContainer('temporal', 'temporalio/temporal:latest')
  .withEndpoint({ port: 7233, targetPort: 7233, name: 'grpc' })
  .withHttpEndpoint({ port: 8233, targetPort: 8233, name: 'ui' })
  .withArgs(['server', 'start-dev', '--ip', '0.0.0.0']);

// TASK-1.4.3: Wire Backend and Frontend
export const backend = await builder
  .addGolangApp('backend', '../backend', 'cmd/api/main.go')
  .withReference(budgetDb)
  .withEnvironment('DEX_ISSUER_URL', 'http://localhost:5556/dex')
  // The Vite dev server runs on 5173 and calls the API on 8080, which is a
  // different origin, so it has to be allowlisted explicitly.
  .withEnvironment('CORS_ALLOWED_ORIGINS', 'http://localhost:5173,http://127.0.0.1:5173')
  .withEnvironment('TEMPORAL_ADDRESS', 'localhost:7233')
  .withEnvironment('TEMPORAL_NAMESPACE', temporalNamespace)
  .withEnvironment('VAULT_MASTER_KEY', devVaultMasterKey)
  .withEnvironment('APP_PUBLIC_URL', 'http://localhost:5173')
  .withEnvironment('EMAIL_FROM_ADDRESS', 'Budget App <invites@dev.local>')
  .withEnvironment('PLATFORM_ADMIN_GROUP', 'budget-admins')
  .withEnvironment('PLATFORM_ADMIN_EMAILS', 'admin@dev.local')
  .waitFor(budgetDb)
  .waitFor(dex)
  .waitFor(temporal);

export const worker = await builder
  .addGolangApp('worker', '../backend', 'cmd/worker/main.go')
  .withReference(budgetDb)
  .withEnvironment('TEMPORAL_ADDRESS', 'localhost:7233')
  .withEnvironment('TEMPORAL_NAMESPACE', temporalNamespace)
  .withEnvironment('VAULT_MASTER_KEY', devVaultMasterKey)
  .withEnvironment('APP_PUBLIC_URL', 'http://localhost:5173')
  .withEnvironment('EMAIL_FROM_ADDRESS', 'Budget App <invites@dev.local>')
  .withEnvironment('PLATFORM_ADMIN_GROUP', 'budget-admins')
  .withEnvironment('PLATFORM_ADMIN_EMAILS', 'admin@dev.local')
  .waitFor(budgetDb)
  .waitFor(temporal)
  .waitFor(backend);

export const frontend = await builder
  .addExecutable('frontend', 'npm', '../frontend', ['run', 'dev'])
  .withHttpEndpoint({ port: 5173, targetPort: 5173, isProxied: false })
  .withEnvironment('BACKEND_URL', 'http://localhost:8080')
  .waitFor(backend);

await builder.build().run();
