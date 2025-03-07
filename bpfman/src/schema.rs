// @generated automatically by Diesel CLI.

diesel::table! {
    bpf_links (id) {
        id -> BigInt,
        program_id -> BigInt,
        link_type -> Nullable<Text>,
        target -> Nullable<Text>,
        state -> Text,
        created_at -> Timestamp,
        updated_at -> Timestamp,
    }
}

diesel::table! {
    bpf_maps (id) {
        id -> BigInt,
        name -> Text,
        map_type -> Nullable<Text>,
        key_size -> Nullable<Integer>,
        value_size -> Nullable<Integer>,
        max_entries -> Nullable<Integer>,
        created_at -> Timestamp,
        updated_at -> Timestamp,
    }
}

diesel::table! {
    bpf_program_maps (program_id, map_id) {
        program_id -> BigInt,
        map_id -> BigInt,
    }
}

diesel::table! {
    bpf_programs (id) {
        id -> BigInt,
        name -> Text,
        description -> Nullable<Text>,
        kind -> Text,
        state -> Text,
        location_type -> Text,
        file_path -> Nullable<Text>,
        image_url -> Nullable<Text>,
        image_pull_policy -> Nullable<Text>,
        username -> Nullable<Text>,
        password -> Nullable<Text>,
        map_pin_path -> Text,
        map_owner_id -> Nullable<Integer>,
        program_bytes -> Binary,
        metadata -> Text,
        global_data -> Text,
        retprobe -> Nullable<Bool>,
        fn_name -> Nullable<Text>,
        kernel_name -> Nullable<Text>,
        kernel_program_type -> Nullable<Integer>,
        kernel_loaded_at -> Nullable<Text>,
        kernel_tag -> Nullable<Text>,
        kernel_gpl_compatible -> Nullable<Bool>,
        kernel_btf_id -> Nullable<Integer>,
        kernel_bytes_xlated -> Nullable<Integer>,
        kernel_jited -> Nullable<Bool>,
        kernel_bytes_jited -> Nullable<Integer>,
        kernel_verified_insns -> Nullable<Integer>,
        kernel_map_ids -> Text,
        kernel_bytes_memlock -> Nullable<Integer>,
        created_at -> Timestamp,
        updated_at -> Timestamp,
    }
}

diesel::joinable!(bpf_links -> bpf_programs (program_id));
diesel::joinable!(bpf_program_maps -> bpf_maps (map_id));
diesel::joinable!(bpf_program_maps -> bpf_programs (program_id));

diesel::allow_tables_to_appear_in_same_query!(bpf_links, bpf_maps, bpf_program_maps, bpf_programs,);
