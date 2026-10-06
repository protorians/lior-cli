import {ModuleApiService} from "@liorian/sdk/infrastructure/module-runtime/module-api.service";
import {FetchResponseWithMetaInterface} from "@liorian/sdk/domain/typing/response";
import {AcmeSyncStateInterface} from "../../domain/acme-sync.interface";

export class AcmeSyncApiService extends ModuleApiService {
    static async getState(options?: Record<string, any>) {
        return await this.get<FetchResponseWithMetaInterface<AcmeSyncStateInterface>>('/acme-sync/state', options);
    }

    static async synchronize() {
        return await this.post<FetchResponseWithMetaInterface<AcmeSyncStateInterface>>('/acme-sync/synchronize', {});
    }
}
