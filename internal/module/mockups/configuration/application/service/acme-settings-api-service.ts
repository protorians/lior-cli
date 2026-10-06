import {ModuleApiService} from "@liorian/sdk/infrastructure/module-runtime/module-api.service";
import {FetchResponseWithMetaInterface} from "@liorian/sdk/domain/typing/response";
import {AcmeSettingsInterface} from "../../domain/acme-settings.interface";

export class AcmeSettingsApiService extends ModuleApiService {
    static async get(options?: Record<string, any>) {
        return await this.get<FetchResponseWithMetaInterface<AcmeSettingsInterface>>('/acme-settings/', options);
    }

    static async update(payload: Partial<AcmeSettingsInterface>) {
        return await this.put<FetchResponseWithMetaInterface<AcmeSettingsInterface>>('/acme-settings/', payload);
    }
}
