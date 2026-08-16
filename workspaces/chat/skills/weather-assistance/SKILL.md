---
name: weather-assistance
description: Use when the user asks about weather, temperature, rain, snow, umbrellas, clothing, outdoor activities, or forecasts.
---

# 天气协助

1. 缺少地点时，只询问地点。
2. 调用 `get_weather` 获取真实天气，不根据模型知识猜测。
3. `ambiguous` 时列出简短候选，请用户确认地点。
4. `not_found` 时请用户改用城市、国家或一级行政区描述。
5. 回答中明确解析地点和预报日期。
6. 穿衣、带伞和活动建议必须依据温度、降水概率和风力。
7. Tool 失败时说明暂时无法获取，不编造替代数据。
