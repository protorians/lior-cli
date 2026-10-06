import {ModuleApiService} from "@liorian/sdk/infrastructure/module-runtime/module-api.service";
import {FetchResponseWithMetaInterface} from "@liorian/sdk/domain/typing/response";
import {SystemConsoleHealthInterface} from "../../domain/system-console.interface";

export class SystemConsoleApiService extends ModuleApiService {
    static async getHealth(options?: Record<string, any>) {
        return await this.get<FetchResponseWithMetaInterface<SystemConsoleHealthInterface>>('/system-console/health', options);
    }
}
