// =============================================================================
// 서버 JSON 응답과 일치하는 타입 정의
// =============================================================================

export interface ResourceGroup {
  code: string;
  name: string;
  definition?: string;
  sort_order: number;
  active: boolean;
}

export interface Subscription {
  id: string;
  user_id: string;
  active: boolean;
  channel_id?: string;
  resource_groups: string[];
  create_at: number;
  update_at: number;
  delete_at: number;
}

export interface GetSubscriptionResponse {
  subscribed: boolean;
  subscription?: Subscription;
  resource_groups: ResourceGroup[];
  delivery_time: string;
}

export interface PutSubscriptionRequest {
  active: boolean;
  channel_id?: string;
  resource_groups: string[];
}

export interface DeliveryLogItem {
  id: string;
  user_id: string;
  subscription_id: string;
  report_id: string;
  channel_id?: string;
  status: "pending" | "sent" | "failed";
  retry_count: number;
  error?: string;
  sent_at?: number;
  create_at: number;
  update_at: number;
}

export interface VisibilityResponse {
  visible: boolean;
  isAdmin: boolean;
}

export interface AdminResourceGroupRequest {
  code: string;
  name: string;
  definition?: string;
  sort_order: number;
  active: boolean;
}

export interface ApiErrorBody {
  success: false;
  error: { code: string; message: string };
}
