import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {AcmeSyncApiService} from "./application/service/acme-sync-api-service";

/**
 * Module `SERVICE` (spec module-types §2.1) : module de service d'arrière-plan,
 * sans UI principale. Ses routines s'ajoutent à la liste des routines
 * (`HeaderRoutines`) quand les vérifications du registre passent.
 *
 * Les routines d'un module `SERVICE` sont déclarées dans `routines.tsx`
 * (contrat first-party) : le socle les fusionne dans la déclaration via le
 * registre `SOCLE_SERVICE_ROUTINES` — ne pas les redéclarer ici.
 *
 * Contrat tiers : descripteurs `routines[]` du manifeste (`job.kind: "api"`,
 * `intervalMs` borné 30 s–1 h), exécutés par le socle via `ctx.api`.
 */
const acmeSyncModule: ModuleDeclarationInterface = {
    identifier: 'service.liorian.acme-sync',
    key: 'ACME_SYNC',
    version: '1.0.0',
    name: 'Acme Sync',
    description: 'Module SERVICE d\'exemple : routine d\'arrière-plan injectée dans la liste des routines',
    icon: "RefreshCwIcon",
    logo: undefined,
    service: {
        fetch: AcmeSyncApiService
    },
    uri: '/acme-sync',
    isEnabled: true,
    isDefault: false,
    type: 'SERVICE',
    external: false,
    category: 'AUTOMATION',
    menu: {
        items: []
    },
}

export default acmeSyncModule
