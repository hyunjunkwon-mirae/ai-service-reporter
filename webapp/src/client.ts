// =============================================================================
// AI Service Reporter REST API 클라이언트
//
// 세션 쿠키 (MMAUTHTOKEN) 자동 첨부.
// =============================================================================

import getSiteURL from "./utils";
import { id as PluginId } from "./manifest";
import {
  GetSubscriptionResponse,
  PutSubscriptionRequest,
  DeliveryLogItem,
  ApiErrorBody,
  ResourceGroup,
  AdminResourceGroupRequest,
  VisibilityResponse,
} from "./types/api";

const BASE = () => `${getSiteURL()}/plugins/${PluginId}/api`;

async function callJSON<T>(
  url: string,
  init: RequestInit = {}
): Promise<T> {
  const resp = await fetch(url, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "XMLHttpRequest",
      ...(init.headers || {}),
    },
  });

  const ct = resp.headers.get("content-type") || "";
  if (!resp.ok) {
    if (ct.includes("application/json")) {
      const err = (await resp.json()) as ApiErrorBody;
      throw new Error(err?.error?.message || `HTTP ${resp.status}`);
    }
    const txt = await resp.text();
    throw new Error(txt || `HTTP ${resp.status}`);
  }
  return (await resp.json()) as T;
}

export class ApiClient {
  // ----- 가시성 + 권한 -----
  static getVisibility(): Promise<VisibilityResponse> {
    return callJSON<VisibilityResponse>(`${BASE()}/visibility`, { method: "GET" });
  }

  // ----- 구독 CRUD -----
  static getSubscription(): Promise<GetSubscriptionResponse> {
    return callJSON<GetSubscriptionResponse>(`${BASE()}/subscription`, { method: "GET" });
  }
  static putSubscription(body: PutSubscriptionRequest): Promise<GetSubscriptionResponse> {
    return callJSON<GetSubscriptionResponse>(`${BASE()}/subscription`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  static deleteSubscription(): Promise<{ success: boolean; message: string }> {
    return callJSON(`${BASE()}/subscription`, { method: "DELETE" });
  }

  // ----- 발송 이력 -----
  static getMyDeliveryLog(limit = 50): Promise<{ items: DeliveryLogItem[] }> {
    return callJSON(`${BASE()}/delivery-log/me?limit=${limit}`, { method: "GET" });
  }

  // ----- 관리자: 리소스그룹 CRUD -----
  static adminListResourceGroups(): Promise<{ items: ResourceGroup[] }> {
    return callJSON(`${BASE()}/admin/resourcegroups`, { method: "GET" });
  }
  static adminCreateResourceGroup(body: AdminResourceGroupRequest): Promise<ResourceGroup> {
    return callJSON(`${BASE()}/admin/resourcegroups`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }
  static adminUpdateResourceGroup(code: string, body: AdminResourceGroupRequest): Promise<ResourceGroup> {
    return callJSON(`${BASE()}/admin/resourcegroups/${encodeURIComponent(code)}`, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }
  static adminDeleteResourceGroup(code: string): Promise<{ success: boolean; code: string }> {
    return callJSON(`${BASE()}/admin/resourcegroups/${encodeURIComponent(code)}`, {
      method: "DELETE",
    });
  }

  // 🚨 TEST-ONLY: 운영 안정화 후 제거 예정 -------------------------------
  static adminSeedAll(): Promise<{
    resource_group_count: number;
    message: string;
  }> {
    return callJSON(`${BASE()}/admin/test-data/seed-all`, { method: "POST" });
  }
  static adminWipeAllTables(): Promise<{
    deleted: Record<string, number>;
    message: string;
  }> {
    return callJSON(`${BASE()}/admin/test-data/wipe`, { method: "POST" });
  }
  // ----------------------------------------------------------------------
}
