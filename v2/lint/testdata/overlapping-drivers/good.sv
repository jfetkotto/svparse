typedef struct packed { logic [3:0] hi; logic [3:0] lo; } pair_t;
module top (input clk, input [7:0] a, input [3:0] b, input [1:0] sel, output logic [7:0] q);
  logic [7:0] v;
  logic [3:0] mem [4];
  pair_t p;
  wire [7:0] n;
  assign v[3:0] = a[3:0];
  assign v[7:4] = a[7:4];
  always_ff @(posedge clk) q[7:4] <= b;
  always_ff @(posedge clk) q[3:0] <= a[3:0];
  always_comb mem[0] = b;
  assign mem[1] = b;
  assign p.hi = b;
  assign p.lo = b;
  assign n[3:0] = a[3:0];
  assign n[3:0] = b;
  logic [3:0] dyn;
  assign dyn[sel] = 1'b0;
  always @(posedge clk) dyn[0] <= 1'b1;
  logic [1:0] t2;
  always @(posedge clk) t2[0] <= 1'b1;
  always @(negedge clk) t2[0] <= 1'b0;
  for (genvar i = 0; i < 2; i++) begin : g
    logic [1:0] t;
    assign t[i] = a[i];
  end
  logic [7:0] bus;
  for (genvar j = 0; j < 8; j++) begin : h
    assign bus[j] = a[j];
  end
endmodule
