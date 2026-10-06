import {AcmeSyncStatus} from "./enums/acme-sync-status.enum";

export interface AcmeSyncStateInterface {
    id: string;
    organizationId: string;
    status: AcmeSyncStatus;
    pendingChanges: number;
    lastRunAt?: string | null;
    synchronizedAt?: string | null;
}
