import {Routine} from "@liorian/sdk/infrastructure/routines/routine";
import {AcmeSyncApiService} from "./application/service/acme-sync-api-service";
import {AcmeSyncStateInterface} from "./domain/acme-sync.interface";

/**
 * Contrat `routines.tsx` d'un module `SERVICE` compilé dans le socle
 * (feature service-routine §3) : un tableau de singletons `Routine` exporté
 * par défaut, fusionné dans la déclaration du module par le registre
 * `SOCLE_SERVICE_ROUTINES`, puis injecté dans la liste des routines
 * (`HeaderRoutines`) quand les vérifications passent (V-1 → V-7).
 */
export class AcmeSyncRoutine extends Routine<AcmeSyncStateInterface> {

    constructor() {
        super('acme-sync.synchronize', {
            icon: 'RefreshCw',
            name: 'Synchronisation Acme Sync en arrière-plan',
            // Persistante par défaut pour un SERVICE : elle survit aux
            // changements de module. Une condition de déclenchement la rend
            // non persistante — active sur les routes listées ou à
            // l'ouverture des modules listés (union des deux critères) :
            // trigger: {url: ['/m/acme-sync'], modules: ['mod.liorian.crm']},
        });
    }

    async job(): Promise<AcmeSyncStateInterface | undefined> {
        const response = await AcmeSyncApiService.synchronize();
        if (
            (!response) ||
            (!response.data) ||
            (!response.data.data)
        ) throw new Error('Impossible de synchroniser les données du module Acme Sync');
        return response.data.data;
    }

    onFail(error: Error) {
        this.setOption('icon', 'MessageCircleWarning')
    }

}

export default [new AcmeSyncRoutine()];
