import {ModuleApiService} from "@liorian/sdk/infrastructure/module-runtime/module-api.service";
import {FetchResponseWithMetaInterface} from "@liorian/sdk/domain/typing/response";
import {
    AcmeWidgetsActivityInterface,
    AcmeWidgetsAnalyticsInterface,
} from "../../domain/acme-widgets.interface";

export class AcmeWidgetsApiService extends ModuleApiService {
    static async getAnalytics(options?: Record<string, any>) {
        return await this.get<FetchResponseWithMetaInterface<AcmeWidgetsAnalyticsInterface>>('/acme-widgets/analytics', options);
    }

    static async getRecentActivities(options?: Record<string, any>) {
        return await this.get<FetchResponseWithMetaInterface<AcmeWidgetsActivityInterface[]>>('/acme-widgets/recent', options);
    }
}
