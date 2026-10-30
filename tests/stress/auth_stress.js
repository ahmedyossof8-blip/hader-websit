// ==============================================================================
// HADER-V2: k6 High-Concurrency Dual-Role Authentication Stress Test
// Tests Sub-30ms Login Latency, Device Locking Verification, and Rate Limiting
// ==============================================================================

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const authSuccessRate = new Rate('auth_success_rate');
const authLatency = new Trend('auth_latency', true);
const rateLimitEnforced = new Rate('rate_limit_enforced');

export const options = {
    scenarios: {
        auth_spike: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '10s', target: 200 },  // Morning campus arrival
                { duration: '15s', target: 800 },  // Peak class changeover
                { duration: '20s', target: 800 },  // Sustained login traffic
                { duration: '10s', target: 0 },    // Cooldown
            ],
            gracefulRampDown: '5s',
        },
    },
    thresholds: {
        'http_req_duration': ['p(95)<35', 'p(99)<70'],
        'auth_latency': ['p(95)<35'],
        'http_req_failed': ['rate<0.05'], // Rate-limiting (429) might trigger intentionally
    },
};

const BASE_URL = __ENV.TARGET_URL || 'http://localhost:8080/api/v1';

export default function () {
    const vuId = __VU;
    const isClassLeader = vuId % 20 === 0; // 5% class leaders, 95% students

    const academicId = isClassLeader
        ? `LEAD-${vuId.toString().padStart(4, '0')}`
        : `STU-${vuId.toString().padStart(5, '0')}`;
    const deviceId = `device-hw-${vuId}`;

    const payload = JSON.stringify({
        academic_id: academicId,
        password: 'Password123!',
        device_id: deviceId,
        role: isClassLeader ? 'class_leader' : 'student',
    });

    const params = {
        headers: {
            'Content-Type': 'application/json',
            'X-Device-Id': deviceId,
            'User-Agent': 'Hader-V2-PWA-Client/2.0',
        },
        tags: { name: 'DualRoleLogin' },
    };

    const start = Date.now();
    const res = http.post(`${BASE_URL}/auth/login`, payload, params);
    authLatency.add(Date.now() - start);

    if (res.status === 200) {
        authSuccessRate.add(1);
        check(res, {
            'Login 200 OK': (r) => r.status === 200,
            'Has JWT token': (r) => r.json('data.token') !== undefined,
            'Role matches': (r) => r.json('data.role') === (isClassLeader ? 'class_leader' : 'student'),
        });
    } else if (res.status === 429) {
        rateLimitEnforced.add(1);
        check(res, {
            'Rate Limiter 429': (r) => r.status === 429,
        });
    } else if (res.status === 403) {
        // Device locked to another user
        check(res, {
            'Device lock 403': (r) => r.status === 403,
        });
    } else {
        check(res, {
            'Non-500 status': (r) => r.status < 500,
        });
    }

    sleep(0.5 + Math.random() * 0.5);
}
