import {useQuery} from "@tanstack/react-query";
import {useAuth} from "@liorian/sdk/infrastructure/hooks/use-auth";
import {HelloWorldApiService} from "../service/hello-world-api-service";
import {HelloWorldAnalyticsInterface} from "../../domain/hello-world.interface";

/**
 * Hook d'analytique du module : encapsule la requête react-query vers le
 * service d'API du module. Les vues et les widgets consomment les mêmes
 * données par le même chemin (clé de query partagée, invalidation unique).
 */
export function useHelloWorldAnalytics() {
    const {currentOrganization} = useAuth();

    return useQuery<HelloWorldAnalyticsInterface>({
        queryKey: ['hello-world', 'analytics'],
        enabled: !!currentOrganization?.id,
        queryFn: async () => {
            const response = await HelloWorldApiService.getAnalytics();
            const payload = response.data as any;
            return payload?.data ?? payload;
        },
    });
}
