import { createBuilder } from './.aspire/modules/aspire.mjs';

const builder = await createBuilder();

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
  .waitFor(budgetDb)
  .waitFor(dex)
  .waitFor(temporal);

export const frontend = await builder
  .addExecutable('frontend', 'npm', '../frontend', ['run', 'dev'])
  .withHttpEndpoint({ port: 5173, targetPort: 5173, isProxied: false })
  .withEnvironment('BACKEND_URL', 'http://localhost:8080')
  .waitFor(backend);

await builder.build().run();
