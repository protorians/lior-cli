import {Routine} from "@liorian/sdk/infrastructure/routines/routine";
import {HelloWorldApiService} from "../../application/service/hello-world-api-service";
import {HelloWorldAnalyticsInterface} from "../../domain/hello-world.interface";

export class HelloWorldAnalyticsRoutine extends Routine<HelloWorldAnalyticsInterface> {

    constructor() {
        super('hello-world.analytics', {
            icon: 'WandSparkles',
            name: 'Service Analytique Hello World en temps réel',
            // Sans `trigger`, la routine est persistante : elle survit aux
            // changements de module. Pour la rendre non persistante — active
            // uniquement sur certaines routes ou à l'ouverture d'un module
            // (contrat `SERVICE`, spec service-routine §3.1) :
            // trigger: {url: ['/hello-world'], modules: ['mod.liorian.crm']},
        });
    }

    async job(): Promise<HelloWorldAnalyticsInterface | undefined> {
        return new Promise(async (resolve) => {
            const response = await HelloWorldApiService.getAnalytics({
                granularity: this.granularity,
            });
            if (
                (!response) ||
                (!response.data) ||
                (!response.data.data)
            ) throw new Error('Impossible de charger les données analytiques du module Hello World');
            resolve(response.data.data);
        })
    }

    onFail(error: Error) {
        this.setOption('icon', 'MessageCircleWarning')
    }

}

export const helloWorldAnalyticsRoutine = new HelloWorldAnalyticsRoutine();