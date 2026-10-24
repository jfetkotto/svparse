module top #(parameter W = 8) (input [3:0] nibble, input [7:0] byte_in, output logic [15:0] q, output logic [7:0] r, output logic [W-1:0] p);
  always_comb begin
    q = 0;
    q = 1'b1;
    q = W;
    r = 8'h0F;
    r = '0;
    p = {byte_in};
    r = {4'b0, nibble};
  end
endmodule
module carry (input [7:0] a, b, input cin, output [7:0] sum, output cout, output [15:0] prod);
  assign {cout, sum} = a + b + cin;
  assign prod = a * b;
endmodule
module arith_shift (input logic signed [23:0] acc, output logic signed [23:0] shifted);
  assign shifted = acc >>> 3;
endmodule
module pick (input logic sel, output logic [31:0] result);
  assign result = sel ? 32'd1 : 32'd0;
endmodule
