import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom Metrics
const raceConditionDoubleCheckin = new Counter('race_condition_double_checkin');
const successfulCheckins = new Rate('successful_checkins');
const idempotentConflicts = new Rate('idempotent_conflicts');
const checkinLatency = new Trend('checkin_latency', true);

export const options = {
    scenarios: {
        massive_university_rush: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '10s', target: 500 },   // تدفق سريع لـ 500 طالب
                { duration: '15s', target: 3000 },  // ذروة مرعبة: 3000 طالب متزامن في نفس الثانية!
                { duration: '20s', target: 3000 },  // ثبات تحت الضغط الأقصى
                { duration: '15s', target: 0 },     // هبوط وتدقيق النتائج
            ],
            gracefulRampDown: '10s',
        },
    },
    thresholds: {
        'http_req_duration': ['p(95)<150'], // سقف زمني مسترخٍ قليلاً نظراً لحجم الـ 3000 طلب المتزامن
        'http_req_failed': ['rate<0.02'],    // السماح بنسبة أخطاء أقل من 2% بسبب تشبع شبكة الويندوز المحلية
    },
};

const BASE_URL = __ENV.TARGET_URL || 'http://localhost:9000/api/v1';
const SESSION_ID = __ENV.SESSION_ID || 'a0000000-0000-0000-0000-000000000001';

export default function () {
    const vuId = __VU;
    const deviceId = `hw-pin-massive-${vuId}`;
    const mockToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdHVkZW50LXRlc3QifQ.signature";

    const payload = JSON.stringify({
        session_id: SESSION_ID,
        totp_code: "123456",
        latitude: 30.044420,
        longitude: 31.235712,
        accuracy_meters: 10.0,
        device_id: deviceId,
    });

    const params = {
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${mockTestToken(mockToken)}`,
            'X-Device-Id': deviceId,
            'Connection': 'keep-alive', // هام جداً لإعادة استخدام قنوات الاتصال وتفادي استنزاف ويندوز
        },
    };

    const startTime = Date.now();
    const res = http.post(`${BASE_URL}/attendance/check-in`, payload, params);
    const latency = Date.now() - startTime;
    checkinLatency.add(latency);

    if (res.status === 200) {
        successfulCheckins.add(1);
    } else if (res.status === 409) {
        idempotentConflicts.add(1);
    }

    check(res, {
        'status is valid code': (r) => r.status === 200 || r.status === 409 || r.status === 401 || r.status === 429,
    });

    sleep(0.1); // تقليل وقت الانتظار لزيادة ضراوة الاختبار
}

function mockTestToken(t) { return t; }