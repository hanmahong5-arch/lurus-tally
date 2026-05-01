-- Nursery dictionary seed data — 200 common species
-- Sources: 中国植物志 (Flora of China), public botanical databases
-- Loaded when SEED_NURSERY_DICT=true at startup
-- tenant_id = '00000000-0000-0000-0000-000000000000' = shared / public seed

-- ============================================================
-- TREES (乔木) — 40 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '红枫', 'Acer palmatum ''Atropurpureum''', '槭树科', 'Acer', 'tree', false, '{华东,华北,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '银杏', 'Ginkgo biloba', '银杏科', 'Ginkgo', 'tree', false, '{华东,华北,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '广玉兰', 'Magnolia grandiflora', '木兰科', 'Magnolia', 'tree', true, '{华南,华东}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '雪松', 'Cedrus deodara', '松科', 'Cedrus', 'tree', true, '{华北,西北,华东}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '水杉', 'Metasequoia glyptostroboides', '柏科', 'Metasequoia', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '法国梧桐', 'Platanus × acerifolia', '悬铃木科', 'Platanus', 'tree', false, '{华东,华北,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '国槐', 'Sophora japonica', '豆科', 'Sophora', 'tree', false, '{华北,华东,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '垂柳', 'Salix babylonica', '杨柳科', 'Salix', 'tree', false, '{华东,华北,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '龙柏', 'Juniperus chinensis ''Kaizuka''', '柏科', 'Juniperus', 'tree', true, '{华东,华北,华中}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '白蜡', 'Fraxinus chinensis', '木樨科', 'Fraxinus', 'tree', false, '{华北,华东,东北}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '栾树', 'Koelreuteria paniculata', '无患子科', 'Koelreuteria', 'tree', false, '{华北,华东,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '樱花', 'Prunus serrulata', '蔷薇科', 'Prunus', 'tree', false, '{华东,华北,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '紫叶李', 'Prunus cerasifera f. atropurpurea', '蔷薇科', 'Prunus', 'tree', false, '{华北,华东,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '碧桃', 'Prunus persica f. duplex', '蔷薇科', 'Prunus', 'tree', false, '{华北,华东,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '合欢', 'Albizia julibrissin', '豆科', 'Albizia', 'tree', false, '{华东,华北,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '五角枫', 'Acer mono', '槭树科', 'Acer', 'tree', false, '{华北,东北,华东}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '黄连木', 'Pistacia chinensis', '漆树科', 'Pistacia', 'tree', false, '{华北,华东,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '榆树', 'Ulmus pumila', '榆科', 'Ulmus', 'tree', false, '{华北,东北,西北}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '皂荚', 'Gleditsia sinensis', '豆科', 'Gleditsia', 'tree', false, '{华北,华东,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '朴树', 'Celtis sinensis', '榆科', 'Celtis', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '马褂木', 'Liriodendron chinense', '木兰科', 'Liriodendron', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '椴树', 'Tilia tuan', '椴树科', 'Tilia', 'tree', false, '{华北,东北,华东}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '白玉兰', 'Magnolia denudata', '木兰科', 'Magnolia', 'tree', false, '{华东,华中,华北}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '紫玉兰', 'Magnolia liliiflora', '木兰科', 'Magnolia', 'tree', false, '{华东,华中}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '悬铃木', 'Platanus occidentalis', '悬铃木科', 'Platanus', 'tree', false, '{华东,华北}', '{3,4}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '杜仲', 'Eucommia ulmoides', '杜仲科', 'Eucommia', 'tree', false, '{华中,华东,华北}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '乌桕', 'Triadica sebifera', '大戟科', 'Triadica', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '香樟', 'Cinnamomum camphora', '樟科', 'Cinnamomum', 'tree', true, '{华南,华东,华中}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '桂花', 'Osmanthus fragrans', '木樨科', 'Osmanthus', 'tree', true, '{华东,华中,华南}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '女贞', 'Ligustrum lucidum', '木樨科', 'Ligustrum', 'tree', true, '{华东,华南,华中}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '枫香', 'Liquidambar formosana', '金缕梅科', 'Liquidambar', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '红花檵木乔木型', 'Loropetalum chinense var. rubrum', '金缕梅科', 'Loropetalum', 'tree', true, '{华南,华东}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '鸡爪槭', 'Acer palmatum', '槭树科', 'Acer', 'tree', false, '{华东,华中}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '红叶乔木', 'Cercidiphyllum japonicum', '连香树科', 'Cercidiphyllum', 'tree', false, '{华东,华北}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '重阳木', 'Bischofia polycarpa', '大戟科', 'Bischofia', 'tree', false, '{华东,华中,华南}', '{3,5}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '柏树', 'Cupressus funebris', '柏科', 'Cupressus', 'tree', true, '{华中,华南,西南}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '侧柏', 'Platycladus orientalis', '柏科', 'Platycladus', 'tree', true, '{华北,华东,华中}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '圆柏', 'Juniperus chinensis', '柏科', 'Juniperus', 'tree', true, '{华北,华东,华中}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '马尾松', 'Pinus massoniana', '松科', 'Pinus', 'tree', true, '{华东,华中,华南}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}'),
('00000000-0000-0000-0000-000000000000', '湿地松', 'Pinus elliottii', '松科', 'Pinus', 'tree', true, '{华南,华东}', '{9,11}', '{"胸径_cm": null, "冠幅_cm": null, "高度_cm": null, "分枝点_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- SHRUBS (灌木) — 35 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '月季', 'Rosa chinensis', '蔷薇科', 'Rosa', 'shrub', false, '{华东,华北,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '紫薇', 'Lagerstroemia indica', '千屈菜科', 'Lagerstroemia', 'shrub', false, '{华东,华北,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '连翘', 'Forsythia suspensa', '木樨科', 'Forsythia', 'shrub', false, '{华北,华东,华中}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '迎春', 'Jasminum nudiflorum', '木樨科', 'Jasminum', 'shrub', false, '{华北,华东,华中}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '石楠', 'Photinia serrulata', '蔷薇科', 'Photinia', 'shrub', true, '{华东,华中,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '红叶石楠', 'Photinia × fraseri', '蔷薇科', 'Photinia', 'shrub', true, '{华东,华中,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '大叶黄杨', 'Buxus megistophylla', '黄杨科', 'Buxus', 'shrub', true, '{华东,华中,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '金叶女贞', 'Ligustrum × vicaryi', '木樨科', 'Ligustrum', 'shrub', true, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '木槿', 'Hibiscus syriacus', '锦葵科', 'Hibiscus', 'shrub', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '海棠', 'Malus spectabilis', '蔷薇科', 'Malus', 'shrub', false, '{华北,华东,华中}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '棣棠', 'Kerria japonica', '蔷薇科', 'Kerria', 'shrub', false, '{华东,华中,华南}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '榆叶梅', 'Prunus triloba', '蔷薇科', 'Prunus', 'shrub', false, '{华北,东北,华东}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '紫荆', 'Cercis chinensis', '豆科', 'Cercis', 'shrub', false, '{华北,华东,华中}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '八角金盘', 'Fatsia japonica', '五加科', 'Fatsia', 'shrub', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '栀子花', 'Gardenia jasminoides', '茜草科', 'Gardenia', 'shrub', true, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '金丝桃', 'Hypericum monogynum', '金丝桃科', 'Hypericum', 'shrub', true, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '夹竹桃', 'Nerium oleander', '夹竹桃科', 'Nerium', 'shrub', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '杜鹃', 'Rhododendron simsii', '杜鹃花科', 'Rhododendron', 'shrub', true, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '山茶', 'Camellia japonica', '山茶科', 'Camellia', 'shrub', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '茉莉', 'Jasminum sambac', '木樨科', 'Jasminum', 'shrub', true, '{华南,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '蜡梅', 'Chimonanthus praecox', '腊梅科', 'Chimonanthus', 'shrub', false, '{华东,华中,华北}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '丁香', 'Syringa oblata', '木樨科', 'Syringa', 'shrub', false, '{华北,东北,华东}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '绣球', 'Hydrangea macrophylla', '绣球花科', 'Hydrangea', 'shrub', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '海桐', 'Pittosporum tobira', '海桐花科', 'Pittosporum', 'shrub', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '小叶女贞', 'Ligustrum quihoui', '木樨科', 'Ligustrum', 'shrub', true, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '南天竹', 'Nandina domestica', '小檗科', 'Nandina', 'shrub', true, '{华东,华中,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '火棘', 'Pyracantha fortuneana', '蔷薇科', 'Pyracantha', 'shrub', true, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '胡枝子', 'Lespedeza bicolor', '豆科', 'Lespedeza', 'shrub', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '锦带花', 'Weigela florida', '忍冬科', 'Weigela', 'shrub', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '麻叶绣球', 'Spiraea cantoniensis', '蔷薇科', 'Spiraea', 'shrub', false, '{华东,华中,华南}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '珊瑚树', 'Viburnum odoratissimum', '忍冬科', 'Viburnum', 'shrub', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '花石榴', 'Punica granatum f. multiplex', '石榴科', 'Punica', 'shrub', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '云南黄馨', 'Jasminum mesnyi', '木樨科', 'Jasminum', 'shrub', true, '{华东,华南,西南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '小叶黄杨', 'Buxus sinica', '黄杨科', 'Buxus', 'shrub', true, '{华东,华北,华中}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}'),
('00000000-0000-0000-0000-000000000000', '红千层', 'Callistemon rigidus', '桃金娘科', 'Callistemon', 'shrub', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "枝条数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- HERBS (地被/草本) — 30 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '麦冬', 'Ophiopogon japonicus', '百合科', 'Ophiopogon', 'herb', true, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '鸢尾', 'Iris tectorum', '鸢尾科', 'Iris', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '玉簪', 'Hosta plantaginea', '百合科', 'Hosta', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '萱草', 'Hemerocallis fulva', '百合科', 'Hemerocallis', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '石竹', 'Dianthus chinensis', '石竹科', 'Dianthus', 'herb', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '天竺葵', 'Pelargonium hortorum', '牻牛儿苗科', 'Pelargonium', 'herb', false, '{华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '一串红', 'Salvia splendens', '唇形科', 'Salvia', 'herb', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '矮牵牛', 'Petunia hybrida', '茄科', 'Petunia', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '金盏菊', 'Calendula officinalis', '菊科', 'Calendula', 'herb', false, '{华东,华北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '孔雀草', 'Tagetes patula', '菊科', 'Tagetes', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '紫茉莉', 'Mirabilis jalapa', '紫茉莉科', 'Mirabilis', 'herb', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '美人蕉', 'Canna indica', '美人蕉科', 'Canna', 'herb', false, '{华南,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '羽衣甘蓝', 'Brassica oleracea var. acephala', '十字花科', 'Brassica', 'herb', false, '{华东,华北}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '芝樱', 'Phlox subulata', '花荵科', 'Phlox', 'herb', true, '{华北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '波斯菊', 'Cosmos bipinnatus', '菊科', 'Cosmos', 'herb', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '鼠尾草', 'Salvia officinalis', '唇形科', 'Salvia', 'herb', false, '{华东,华北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '雏菊', 'Bellis perennis', '菊科', 'Bellis', 'herb', false, '{华东,华北}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '三色堇', 'Viola tricolor', '堇菜科', 'Viola', 'herb', false, '{华东,华北}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '凤仙花', 'Impatiens balsamina', '凤仙花科', 'Impatiens', 'herb', false, '{华东,华南,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '向日葵', 'Helianthus annuus', '菊科', 'Helianthus', 'herb', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '五叶地锦草', 'Parthenocissus quinquefolia', '葡萄科', 'Parthenocissus', 'herb', false, '{华北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '草坪草—结缕草', 'Zoysia japonica', '禾本科', 'Zoysia', 'herb', false, '{华东,华南,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '草坪草—高羊茅', 'Festuca arundinacea', '禾本科', 'Festuca', 'herb', true, '{华北,华东,东北}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '草坪草—狗牙根', 'Cynodon dactylon', '禾本科', 'Cynodon', 'herb', false, '{华南,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '沿阶草', 'Ophiopogon bodinieri', '百合科', 'Ophiopogon', 'herb', true, '{华东,华南,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '蒲公英', 'Taraxacum mongolicum', '菊科', 'Taraxacum', 'herb', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '月见草', 'Oenothera biennis', '柳叶菜科', 'Oenothera', 'herb', false, '{华东,华北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '酢浆草', 'Oxalis corniculata', '酢浆草科', 'Oxalis', 'herb', false, '{华东,华南,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '紫花地丁', 'Viola yedoensis', '堇菜科', 'Viola', 'herb', false, '{华北,华东}', '{3,4}', '{"冠幅_cm": null, "高度_cm": null}'),
('00000000-0000-0000-0000-000000000000', '车前草', 'Plantago asiatica', '车前科', 'Plantago', 'herb', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- VINES (藤本) — 20 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '紫藤', 'Wisteria sinensis', '豆科', 'Wisteria', 'vine', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '凌霄', 'Campsis grandiflora', '紫葳科', 'Campsis', 'vine', false, '{华东,华北,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '爬山虎', 'Parthenocissus tricuspidata', '葡萄科', 'Parthenocissus', 'vine', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '常春藤', 'Hedera helix', '五加科', 'Hedera', 'vine', true, '{华东,华中,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '木香', 'Rosa banksiae', '蔷薇科', 'Rosa', 'vine', true, '{华东,华南,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '金银花', 'Lonicera japonica', '忍冬科', 'Lonicera', 'vine', true, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '葫芦', 'Lagenaria siceraria', '葫芦科', 'Lagenaria', 'vine', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '月光花', 'Ipomoea alba', '旋花科', 'Ipomoea', 'vine', false, '{华南,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '素馨', 'Jasminum officinale', '木樨科', 'Jasminum', 'vine', true, '{华南,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '使君子', 'Quisqualis indica', '使君子科', 'Quisqualis', 'vine', false, '{华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '铁线莲', 'Clematis florida', '毛茛科', 'Clematis', 'vine', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '薜荔', 'Ficus pumila', '桑科', 'Ficus', 'vine', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '绿萝（室外藤本）', 'Epipremnum aureum', '天南星科', 'Epipremnum', 'vine', true, '{华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '猕猴桃藤', 'Actinidia chinensis', '猕猴桃科', 'Actinidia', 'vine', false, '{华东,华中,西南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '络石', 'Trachelospermum jasminoides', '夹竹桃科', 'Trachelospermum', 'vine', true, '{华东,华南,华中}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '五叶木通', 'Akebia quinata', '木通科', 'Akebia', 'vine', false, '{华东,华中,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '扶芳藤', 'Euonymus fortunei', '卫矛科', 'Euonymus', 'vine', true, '{华东,华北,华中}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '葡萄藤', 'Vitis vinifera', '葡萄科', 'Vitis', 'vine', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '南蛇藤', 'Celastrus orbiculatus', '卫矛科', 'Celastrus', 'vine', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '美丽胡枝子藤', 'Campsis radicans', '紫葳科', 'Campsis', 'vine', false, '{华东,华北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- BAMBOO (竹类) — 15 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '毛竹', 'Phyllostachys edulis', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华中,华南}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '刚竹', 'Phyllostachys sulphurea', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华中}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '紫竹', 'Phyllostachys nigra', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华中,华南}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '箬竹', 'Indocalamus tessellatus', '禾本科', 'Indocalamus', 'bamboo', true, '{华东,华中,华南}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '凤尾竹', 'Bambusa multiplex ''Fernleaf''', '禾本科', 'Bambusa', 'bamboo', true, '{华南,华东}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '孝顺竹', 'Bambusa multiplex', '禾本科', 'Bambusa', 'bamboo', true, '{华南,华东}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '佛肚竹', 'Bambusa ventricosa', '禾本科', 'Bambusa', 'bamboo', true, '{华南}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '龟甲竹', 'Phyllostachys edulis f. heterocycla', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华中}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '斑竹', 'Phyllostachys bambusoides f. tanakae', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华中}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '淡竹', 'Phyllostachys glauca', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华北,华中}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '早竹', 'Phyllostachys praecox', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '罗汉竹', 'Phyllostachys aurea', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东,华南}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '黄杆乌哺鸡竹', 'Phyllostachys vivax f. aureocaulis', '禾本科', 'Phyllostachys', 'bamboo', true, '{华东}', '{3,5}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '寒竹', 'Chimonobambusa marmorea', '禾本科', 'Chimonobambusa', 'bamboo', true, '{华东,华中}', '{9,11}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}'),
('00000000-0000-0000-0000-000000000000', '方竹', 'Chimonobambusa quadrangularis', '禾本科', 'Chimonobambusa', 'bamboo', true, '{华东,华中,华南}', '{9,11}', '{"竿高_cm": null, "竿径_cm": null, "丛数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- AQUATIC (水生) — 15 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '睡莲', 'Nymphaea tetragona', '睡莲科', 'Nymphaea', 'aquatic', false, '{华东,华北,华中,华南}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '荷花', 'Nelumbo nucifera', '睡莲科', 'Nelumbo', 'aquatic', false, '{华东,华中,华南}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '水葱', 'Schoenoplectus tabernaemontani', '莎草科', 'Schoenoplectus', 'aquatic', false, '{华东,华北,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '千屈菜', 'Lythrum salicaria', '千屈菜科', 'Lythrum', 'aquatic', false, '{华东,华北,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '水生美人蕉', 'Canna glauca', '美人蕉科', 'Canna', 'aquatic', false, '{华东,华南}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '黄菖蒲', 'Iris pseudacorus', '鸢尾科', 'Iris', 'aquatic', false, '{华东,华北}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '再力花', 'Thalia dealbata', '竹芋科', 'Thalia', 'aquatic', false, '{华南,华东}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '梭鱼草', 'Pontederia cordata', '雨久花科', 'Pontederia', 'aquatic', false, '{华东,华南}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '芦苇', 'Phragmites australis', '禾本科', 'Phragmites', 'aquatic', false, '{华北,华东,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '香蒲', 'Typha orientalis', '香蒲科', 'Typha', 'aquatic', false, '{华北,华东,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '水生鸢尾', 'Iris laevigata', '鸢尾科', 'Iris', 'aquatic', false, '{华东,华北}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '花叶芦竹', 'Arundo donax var. versicolor', '禾本科', 'Arundo', 'aquatic', false, '{华东,华南}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '雨久花', 'Monochoria korsakowii', '雨久花科', 'Monochoria', 'aquatic', false, '{华东,华北,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '浮萍', 'Lemna minor', '天南星科', 'Lemna', 'aquatic', false, '{华东,华北,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}'),
('00000000-0000-0000-0000-000000000000', '菖蒲', 'Acorus calamus', '菖蒲科', 'Acorus', 'aquatic', false, '{华东,华北,华中}', '{4,9}', '{"盆径_cm": null, "叶片数": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- BULBS (球根) — 15 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '百合', 'Lilium brownii', '百合科', 'Lilium', 'bulb', false, '{华东,华北,华中}', '{3,4}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '郁金香', 'Tulipa gesneriana', '百合科', 'Tulipa', 'bulb', false, '{华北,华东}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '水仙', 'Narcissus tazetta', '石蒜科', 'Narcissus', 'bulb', false, '{华东,华南}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '风信子', 'Hyacinthus orientalis', '百合科', 'Hyacinthus', 'bulb', false, '{华北,华东}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '大丽花', 'Dahlia pinnata', '菊科', 'Dahlia', 'bulb', false, '{华东,华北,华中}', '{3,5}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '唐菖蒲', 'Gladiolus × gandavensis', '鸢尾科', 'Gladiolus', 'bulb', false, '{华东,华北,华中}', '{3,5}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '石蒜', 'Lycoris radiata', '石蒜科', 'Lycoris', 'bulb', false, '{华东,华中,华南}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '番红花', 'Crocus sativus', '鸢尾科', 'Crocus', 'bulb', false, '{华北,华东}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '朱顶红', 'Hippeastrum vittatum', '石蒜科', 'Hippeastrum', 'bulb', false, '{华东,华南}', '{3,5}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '晚香玉', 'Polianthes tuberosa', '石蒜科', 'Polianthes', 'bulb', false, '{华东,华南,华中}', '{3,5}', '{"球径_cm": null, "球重_g": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '仙客来', 'Cyclamen persicum', '报春花科', 'Cyclamen', 'bulb', false, '{华东,华北}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '葡萄风信子', 'Muscari armeniacum', '百合科', 'Muscari', 'bulb', false, '{华北,华东}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '球根海棠', 'Begonia × tuberhybrida', '秋海棠科', 'Begonia', 'bulb', false, '{华东,华南}', '{3,5}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '花毛茛', 'Ranunculus asiaticus', '毛茛科', 'Ranunculus', 'bulb', false, '{华东,华北}', '{9,11}', '{"球径_cm": null, "球重_g": null}'),
('00000000-0000-0000-0000-000000000000', '鸢尾球根', 'Iris hollandica', '鸢尾科', 'Iris', 'bulb', false, '{华北,华东}', '{9,11}', '{"球径_cm": null, "球重_g": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- FRUIT TREES (果树) — 25 entries
-- ============================================================
INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '苹果', 'Malus pumila', '蔷薇科', 'Malus', 'fruit', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '梨', 'Pyrus bretschneideri', '蔷薇科', 'Pyrus', 'fruit', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '桃', 'Prunus persica', '蔷薇科', 'Prunus', 'fruit', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '李', 'Prunus salicina', '蔷薇科', 'Prunus', 'fruit', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '柿子', 'Diospyros kaki', '柿科', 'Diospyros', 'fruit', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '核桃', 'Juglans regia', '胡桃科', 'Juglans', 'fruit', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '柑橘', 'Citrus reticulata', '芸香科', 'Citrus', 'fruit', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '枇杷', 'Eriobotrya japonica', '蔷薇科', 'Eriobotrya', 'fruit', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '石榴', 'Punica granatum', '石榴科', 'Punica', 'fruit', false, '{华东,华北,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '无花果', 'Ficus carica', '桑科', 'Ficus', 'fruit', false, '{华南,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '猕猴桃', 'Actinidia deliciosa', '猕猴桃科', 'Actinidia', 'fruit', false, '{华东,华中,西南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '枣树', 'Ziziphus jujuba', '鼠李科', 'Ziziphus', 'fruit', false, '{华北,华东,西北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '山楂', 'Crataegus pinnatifida', '蔷薇科', 'Crataegus', 'fruit', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '葡萄（果用）', 'Vitis vinifera (fruit)', '葡萄科', 'Vitis', 'fruit', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '杏', 'Prunus armeniaca', '蔷薇科', 'Prunus', 'fruit', false, '{华北,东北,华东}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '樱桃', 'Prunus avium', '蔷薇科', 'Prunus', 'fruit', false, '{华北,华东,东北}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '橙', 'Citrus sinensis', '芸香科', 'Citrus', 'fruit', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '柠檬', 'Citrus limon', '芸香科', 'Citrus', 'fruit', true, '{华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '香蕉', 'Musa nana', '芭蕉科', 'Musa', 'fruit', false, '{华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '荔枝', 'Litchi chinensis', '无患子科', 'Litchi', 'fruit', true, '{华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO tally.nursery_dict (tenant_id, name, latin_name, family, genus, type, is_evergreen, climate_zones, best_season, spec_template) VALUES
('00000000-0000-0000-0000-000000000000', '龙眼', 'Dimocarpus longan', '无患子科', 'Dimocarpus', 'fruit', true, '{华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '木瓜', 'Chaenomeles sinensis', '蔷薇科', 'Chaenomeles', 'fruit', false, '{华东,华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '柚子', 'Citrus maxima', '芸香科', 'Citrus', 'fruit', true, '{华南,华东}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '芒果', 'Mangifera indica', '漆树科', 'Mangifera', 'fruit', true, '{华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '板栗', 'Castanea mollissima', '壳斗科', 'Castanea', 'fruit', false, '{华北,华东,华中}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '火龙果', 'Selenicereus undatus', '仙人掌科', 'Selenicereus', 'fruit', false, '{华南}', '{3,5}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '番石榴', 'Psidium guajava', '桃金娘科', 'Psidium', 'fruit', true, '{华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '杨梅', 'Morella rubra', '杨梅科', 'Morella', 'fruit', true, '{华东,华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}'),
('00000000-0000-0000-0000-000000000000', '橄榄', 'Canarium album', '橄榄科', 'Canarium', 'fruit', true, '{华南}', '{9,11}', '{"冠幅_cm": null, "高度_cm": null, "嫁接/扦插": null}')
ON CONFLICT (tenant_id, name) DO NOTHING;
